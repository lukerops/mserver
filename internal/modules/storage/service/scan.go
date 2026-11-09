package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
	"github.com/lukerops/mserver/internal/modules/storage/events"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

func (s *service) ScanLocation(ctx context.Context, id uuid.UUID) (ScanResult, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	start := time.Now()

	loc, err := s.store.GetLocation(ctx, id)
	if err != nil {
		return ScanResult{}, err
	}
	d, err := newDriver(loc.Driver, loc.Options)
	if err != nil {
		return ScanResult{}, err
	}

	files, err := s.store.ListFiles(ctx, id)
	if err != nil {
		return ScanResult{}, err
	}
	known := make(map[string]File, len(files))
	for _, f := range files {
		known[f.Path] = f
	}

	var found []driver.FileInfo
	err = d.Walk(ctx, func(info driver.FileInfo) error {
		found = append(found, info)
		return nil
	})
	if err != nil {
		// Unmounted disk or unreadable directory: keep the index untouched.
		return ScanResult{}, fmt.Errorf("%w: scan of location %s failed: %w", ErrUnavailable, id, err)
	}

	changes, evs := diff(id, known, found)
	if len(evs) == 0 {
		s.log.InfoContext(ctx, "storage: location scanned", "location_id", id,
			"files", len(found), "changes", 0, "duration", time.Since(start))
		return ScanResult{}, nil
	}
	if err := s.store.WithTx(ctx, func(ctx context.Context) error {
		if err := s.store.ApplyScan(ctx, id, changes); err != nil {
			return err
		}
		return s.outbox.Publish(ctx, evs...)
	}); err != nil {
		return ScanResult{}, err
	}
	s.logFileChanges(ctx, evs)
	s.kickAnalysis()
	result := scanResult(evs)
	s.log.InfoContext(ctx, "storage: location scanned", "location_id", id,
		"files", len(found), "changes", len(evs), "added", result.Added, "changed", result.Changed,
		"moved", result.Moved, "removed", result.Removed, "duration", time.Since(start))
	return result, nil
}

// fileChangeMessages are the log messages of the file events of a scan.
var fileChangeMessages = map[string]string{
	events.FileIndexedV1: "storage: file indexed",
	events.FileChangedV1: "storage: file updated",
	events.FileMovedV1:   "storage: file moved",
	events.FileRemovedV1: "storage: file removed",
}

// logFileChanges logs, at debug, each file change saved by a scan.
func (s *service) logFileChanges(ctx context.Context, evs []queue.Event) {
	if !s.log.Enabled(ctx, slog.LevelDebug) {
		return
	}
	for _, e := range evs {
		env, ok := e.(queue.Envelope[events.FileEventV1])
		if !ok {
			continue
		}
		s.log.DebugContext(ctx, fileChangeMessages[e.Topic()],
			"file_id", env.Data.FileID, "location_id", env.Data.LocationID, "path", env.Data.Path)
	}
}

// diff compares the index of a location with the files found on disk and
// returns the changes with their events. A new path with the inode of a
// missing file is the same file, moved.
func diff(location uuid.UUID, known map[string]File, found []driver.FileInfo) (store.ScanChanges, []queue.Event) {
	var (
		changes = store.ScanChanges{
			Added:   make(map[uuid.UUID]driver.FileInfo),
			Updated: make(map[uuid.UUID]driver.FileInfo),
		}
		evs   []queue.Event
		newer []driver.FileInfo
		seen  = make(map[string]bool, len(found))
	)

	for _, info := range found {
		seen[info.Path] = true
		f, ok := known[info.Path]
		switch {
		case !ok:
			newer = append(newer, info)
		case f.Size != info.Size || !f.ModTime.Equal(info.ModTime) || f.Inode != info.Inode:
			changes.Updated[f.ID] = info
			evs = append(evs, fileEvent(events.FileChangedV1, location, f.ID, info.Path))
		}
	}

	missing := make(map[uint64]string) // inode -> old path
	for p, f := range known {
		if !seen[p] && f.Inode != 0 {
			missing[f.Inode] = p
		}
	}

	for _, info := range newer {
		if old, ok := missing[info.Inode]; ok && info.Inode != 0 {
			delete(missing, info.Inode)
			seen[old] = true
			id := known[old].ID
			changes.Updated[id] = info
			evs = append(evs, fileEvent(events.FileMovedV1, location, id, info.Path))
			continue
		}
		id := newID()
		changes.Added[id] = info
		evs = append(evs, fileEvent(events.FileIndexedV1, location, id, info.Path))
	}

	var removed []string
	for p := range known {
		if !seen[p] {
			removed = append(removed, p)
		}
	}
	slices.Sort(removed)
	for _, p := range removed {
		id := known[p].ID
		changes.Removed = append(changes.Removed, id)
		evs = append(evs, fileEvent(events.FileRemovedV1, location, id, p))
	}
	return changes, evs
}

func scanResult(evs []queue.Event) ScanResult {
	var r ScanResult
	for _, e := range evs {
		switch e.Topic() {
		case events.FileIndexedV1:
			r.Added++
		case events.FileChangedV1:
			r.Changed++
		case events.FileMovedV1:
			r.Moved++
		case events.FileRemovedV1:
			r.Removed++
		}
	}
	return r
}

func (s *service) ScanAll(ctx context.Context) {
	start := time.Now()
	locations, err := s.store.ListLocations(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.ErrorContext(ctx, "storage: list locations", "error", err)
		}
		return
	}
	failed := 0
	for _, loc := range locations {
		if _, err := s.ScanLocation(ctx, loc.ID); err != nil {
			if ctx.Err() != nil {
				return
			}
			failed++
			s.log.WarnContext(ctx, "storage: scan failed", "location_id", loc.ID, "error", err)
		}
	}
	s.log.DebugContext(ctx, "storage: all locations scanned",
		"count", len(locations), "failed", failed, "duration", time.Since(start))
}

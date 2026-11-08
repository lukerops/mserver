package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
	"github.com/lukerops/mserver/internal/modules/storage/events"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

func (s *service) ScanLocation(ctx context.Context, id uuid.UUID) (ScanResult, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

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
	s.kickAnalysis()
	return scanResult(evs), nil
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
	locations, err := s.store.ListLocations(ctx)
	if err != nil {
		s.log.Error("storage: list locations", "error", err)
		return
	}
	for _, loc := range locations {
		result, err := s.ScanLocation(ctx, loc.ID)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("storage: scan failed", "location", loc.ID, "error", err)
			}
			continue
		}
		s.log.Debug("storage: scan finished", "location", loc.ID, "result", result)
	}
}

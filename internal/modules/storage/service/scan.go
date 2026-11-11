package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

// ScanLocation synchronizes the index with the files of the location. If the
// location is not accessible, the index is left untouched.
func (s *Service) ScanLocation(ctx context.Context, id uuid.UUID) (*ScanResult, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	start := time.Now()

	loc, err := s.store.GetLocation(ctx, id)
	if err != nil {
		return nil, err
	}
	d, err := newDriver(loc.Driver, loc.Options)
	if err != nil {
		return nil, err
	}

	files, err := s.store.ListFiles(ctx, id, store.Page{})
	if err != nil {
		return nil, err
	}
	known := make(map[string]*File, len(files))
	for i := range files {
		known[files[i].Path] = &files[i]
	}

	var found []driver.FileInfo
	err = d.Walk(ctx, func(info driver.FileInfo) error {
		found = append(found, info)
		return nil
	})
	if err != nil {
		// Unmounted disk or unreadable directory: keep the index untouched.
		return nil, fmt.Errorf("%w: scan of location %s failed: %w", ErrUnavailable, id, err)
	}

	scanChanges, changes := diff(known, found)
	if len(changes) == 0 {
		s.log.InfoContext(ctx, "storage: location scanned",
			slog.String("location_id", id.String()),
			slog.Int("files", len(found)),
			slog.Int("changes", 0),
			slog.Int64("duration", int64(time.Since(start))),
		)
		return &ScanResult{}, nil
	}
	if err := s.store.ApplyScan(ctx, id, scanChanges); err != nil {
		return nil, err
	}
	if err := s.publishFileChanges(ctx, id, changes); err != nil {
		s.logPublishError(ctx, err)
	}
	s.logFileChanges(ctx, id, changes)
	s.kickAnalysis()
	result := scanResult(changes)
	s.log.InfoContext(ctx, "storage: location scanned",
		slog.String("location_id", id.String()),
		slog.Int("files", len(found)),
		slog.Int("changes", len(changes)),
		slog.Int("added", result.Added),
		slog.Int("changed", result.Changed),
		slog.Int("moved", result.Moved),
		slog.Int("removed", result.Removed),
		slog.Int64("duration", int64(time.Since(start))),
	)
	return result, nil
}

// fileChangeMessages are the log messages of the file changes of a scan.
var fileChangeMessages = map[string]string{
	events.FileIndexedV1: "storage: file indexed",
	events.FileChangedV1: "storage: file updated",
	events.FileMovedV1:   "storage: file moved",
	events.FileRemovedV1: "storage: file removed",
}

// logFileChanges logs, at debug, each file change saved by a scan.
func (s *Service) logFileChanges(ctx context.Context, location uuid.UUID, changes []fileChange) {
	if !s.log.Enabled(ctx, slog.LevelDebug) {
		return
	}
	for i := range changes {
		c := &changes[i]
		s.log.DebugContext(ctx, fileChangeMessages[c.Type],
			slog.String("file_id", c.ID.String()),
			slog.String("location_id", location.String()),
			slog.String("path", c.Path),
		)
	}
}

// diff compares the index of a location with the files found on disk and
// returns the changes to apply, with the file event of each one. A new path
// with the inode of a missing file is the same file, moved.
func diff(known map[string]*File, found []driver.FileInfo) (*store.ScanChanges, []fileChange) {
	var (
		scanChanges = &store.ScanChanges{
			Added:   make(map[uuid.UUID]driver.FileInfo),
			Updated: make(map[uuid.UUID]driver.FileInfo),
		}
		changes []fileChange
		newer   []driver.FileInfo
		seen    = make(map[string]bool, len(found))
	)

	for _, info := range found {
		seen[info.Path] = true
		f, ok := known[info.Path]
		switch {
		case !ok:
			newer = append(newer, info)
		case f.Size != info.Size || !f.ModTime.Equal(info.ModTime) || f.Inode != info.Inode:
			scanChanges.Updated[f.ID] = info
			changes = append(changes, fileChange{Type: events.FileChangedV1, ID: f.ID, Path: info.Path})
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
			scanChanges.Updated[id] = info
			changes = append(changes, fileChange{Type: events.FileMovedV1, ID: id, Path: info.Path})
			continue
		}
		id := newID()
		scanChanges.Added[id] = info
		changes = append(changes, fileChange{Type: events.FileIndexedV1, ID: id, Path: info.Path})
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
		scanChanges.Removed = append(scanChanges.Removed, id)
		changes = append(changes, fileChange{Type: events.FileRemovedV1, ID: id, Path: p})
	}
	return scanChanges, changes
}

func scanResult(changes []fileChange) *ScanResult {
	var r ScanResult
	for i := range changes {
		switch changes[i].Type {
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
	return &r
}

// ScanAll scans every location, logging failures.
func (s *Service) ScanAll(ctx context.Context) {
	start := time.Now()
	locations, err := s.store.ListLocations(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.ErrorContext(ctx, "storage: list locations", slog.String("error", err.Error()))
		}
		return
	}
	failed := 0
	for i := range locations {
		id := locations[i].ID
		if _, err := s.ScanLocation(ctx, id); err != nil {
			if ctx.Err() != nil {
				return
			}
			failed++
			s.log.WarnContext(ctx, "storage: scan failed",
				slog.String("location_id", id.String()),
				slog.String("error", err.Error()),
			)
		}
	}
	s.log.DebugContext(ctx, "storage: all locations scanned",
		slog.Int("count", len(locations)),
		slog.Int("failed", failed),
		slog.Int64("duration", int64(time.Since(start))),
	)
}

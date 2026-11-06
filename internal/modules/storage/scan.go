package storage

import (
	"context"
	"fmt"
	"slices"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/api"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

func (s *Service) ScanLocation(ctx context.Context, id api.LocationID) (api.ScanResult, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	loc, err := s.store.GetLocation(ctx, id)
	if err != nil {
		return api.ScanResult{}, err
	}
	d, err := newDriver(loc.Driver, loc.Options)
	if err != nil {
		return api.ScanResult{}, err
	}

	files, err := s.store.ListFiles(ctx, id)
	if err != nil {
		return api.ScanResult{}, err
	}
	known := make(map[string]api.File, len(files))
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
		return api.ScanResult{}, fmt.Errorf("%w: scan of location %s failed: %w", api.ErrUnavailable, id, err)
	}

	changes, events := diff(id, known, found)
	if len(events) == 0 {
		return api.ScanResult{}, nil
	}
	if err := s.store.WithTx(ctx, func(ctx context.Context) error {
		if err := s.store.ApplyScan(ctx, id, changes); err != nil {
			return err
		}
		return s.outbox.Publish(ctx, events...)
	}); err != nil {
		return api.ScanResult{}, err
	}
	return scanResult(events), nil
}

// diff compares the index of a location with the files found on disk and
// returns the changes with their events. A new path with the inode of a
// missing file is the same file, moved.
func diff(loc api.LocationID, known map[string]api.File, found []driver.FileInfo) (store.ScanChanges, []queue.Event) {
	var (
		changes = store.ScanChanges{
			Added:   make(map[api.FileID]driver.FileInfo),
			Updated: make(map[api.FileID]driver.FileInfo),
		}
		events []queue.Event
		newer  []driver.FileInfo
		seen   = make(map[string]bool, len(found))
	)

	for _, info := range found {
		seen[info.Path] = true
		f, ok := known[info.Path]
		switch {
		case !ok:
			newer = append(newer, info)
		case f.Size != info.Size || !f.ModTime.Equal(info.ModTime) || f.Inode != info.Inode:
			changes.Updated[f.ID] = info
			events = append(events, newFileEvent(api.EventFileChanged, loc, f.ID, info.Path))
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
			events = append(events, newFileEvent(api.EventFileMoved, loc, id, info.Path))
			continue
		}
		id := api.NewFileID()
		changes.Added[id] = info
		events = append(events, newFileEvent(api.EventFileIndexed, loc, id, info.Path))
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
		events = append(events, newFileEvent(api.EventFileRemoved, loc, id, p))
	}
	return changes, events
}

func scanResult(events []queue.Event) api.ScanResult {
	var r api.ScanResult
	for _, e := range events {
		switch e.Topic() {
		case api.EventFileIndexed:
			r.Added++
		case api.EventFileChanged:
			r.Changed++
		case api.EventFileMoved:
			r.Moved++
		case api.EventFileRemoved:
			r.Removed++
		}
	}
	return r
}

// scanAll scans every location, logging failures.
func (s *Service) scanAll(ctx context.Context) {
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

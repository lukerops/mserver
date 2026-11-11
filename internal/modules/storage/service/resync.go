package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

// ResyncLocation publishes the indexed event of every file of the
// location, followed by the analyzed event of every successful analysis
// of their current content.
func (s *Service) ResyncLocation(ctx context.Context, id uuid.UUID) error {
	// No scan or analysis changes the index while it is read.
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	if _, err := s.store.GetLocation(ctx, id); err != nil {
		return err
	}
	files, err := s.store.ListFiles(ctx, id, store.Page{})
	if err != nil {
		return err
	}
	analyses, err := s.store.ListAnalyses(ctx, id)
	if err != nil {
		return err
	}

	changes := make([]fileChange, len(files))
	for i := range files {
		changes[i] = fileChange{Type: events.FileIndexedV1, ID: files[i].ID, Path: files[i].Path}
	}
	if err := s.publishFileChanges(ctx, id, changes); err != nil {
		return err
	}
	for i := range analyses {
		if err := s.publishAnalysis(ctx, id, &analyses[i]); err != nil {
			return err
		}
	}
	s.log.DebugContext(ctx, "storage: location resynced",
		slog.String("location_id", id.String()),
		slog.Int("files", len(files)),
		slog.Int("analyses", len(analyses)),
	)
	return nil
}

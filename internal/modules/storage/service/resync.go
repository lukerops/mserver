package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/events"
)

// ResyncLocation publishes the indexed event of every file of the
// location, followed by the analyzed event of every successful analysis
// of their current content.
func (s *service) ResyncLocation(ctx context.Context, id uuid.UUID) error {
	// No scan or analysis changes the index while it is read.
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	if _, err := s.store.GetLocation(ctx, id); err != nil {
		return err
	}
	files, err := s.store.ListFiles(ctx, id)
	if err != nil {
		return err
	}

	evs := make([]queue.Event, 0, len(files))
	for _, f := range files {
		evs = append(evs, fileEvent(events.FileIndexedV1, id, f.ID, f.Path))
	}
	return s.outbox.Publish(ctx, evs...)
}

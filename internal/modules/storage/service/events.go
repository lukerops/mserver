package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
)

// fileChange is a change of a file in the index, published as the file
// event of the given type.
type fileChange struct {
	Type string
	ID   uuid.UUID
	Path string
}

// publish wraps data in an envelope and publishes it. The change it
// describes is already committed, so the publication is not canceled with
// the request.
func publish[T any](ctx context.Context, pub pubsub.Publisher, typ, subject string, data T) error {
	msg, err := pubsub.Encode(pubsub.NewEnvelope(typ, events.SourceStorage, subject, data))
	if err != nil {
		return err
	}
	return pub.Publish(context.WithoutCancel(ctx), msg)
}

// publishFileChanges publishes the file events of a location, in order. It
// stops at the first failure.
func (s *Service) publishFileChanges(ctx context.Context, location uuid.UUID, changes []fileChange) error {
	for i := range changes {
		c := &changes[i]
		data := events.FileEventV1{FileID: c.ID, LocationID: location, Path: c.Path}
		if err := publish(ctx, s.pub, c.Type, c.ID.String(), data); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) publishAnalysis(ctx context.Context, location uuid.UUID, a *Analysis) error {
	data := events.FileAnalyzedEventV1{
		FileID:     a.FileID,
		LocationID: location,
		Analyzer:   a.Analyzer,
		Version:    a.Version,
		Result:     a.Result,
	}
	return publish(ctx, s.pub, events.FileAnalyzedV1, a.FileID.String(), data)
}

// logPublishError logs a failed publication after a commit. The state is
// already saved: a resync republishes it.
func (s *Service) logPublishError(ctx context.Context, err error) {
	s.log.ErrorContext(ctx, "storage: publish events failed, a resync reconciles the consumers",
		slog.String("error", err.Error()),
	)
}

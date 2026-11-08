package service

import (
	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/events"
)

func fileEvent(typ string, location, id uuid.UUID, path string) queue.Envelope[events.FileEventV1] {
	return queue.NewEnvelope(typ, events.Source, id.String(), events.FileEventV1{
		FileID:     id,
		LocationID: location,
		Path:       path,
	})
}

func fileAnalyzedEvent(location uuid.UUID, a Analysis) queue.Envelope[events.FileAnalyzedEventV1] {
	return queue.NewEnvelope(events.FileAnalyzedV1, events.Source, a.FileID.String(), events.FileAnalyzedEventV1{
		FileID:     a.FileID,
		LocationID: location,
		Analyzer:   a.Analyzer,
		Version:    a.Version,
		Result:     a.Result,
	})
}

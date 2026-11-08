package service

import (
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/library/events"
)

func movieEvent(typ string, m Movie) queue.Envelope[events.MovieEventV1] {
	e := events.MovieEventV1{
		MovieID:    m.ID,
		LibraryID:  m.LibraryID,
		FileID:     m.FileID,
		LocationID: m.LocationID,
		Path:       m.Path,
		Title:      m.Title,
		Year:       m.Year,
	}
	if m.MediaInfo != nil {
		e.DurationMs = m.MediaInfo.DurationMs
	}
	return queue.NewEnvelope(typ, events.Source, m.ID.String(), e)
}

func movieRemovedEvent(m Movie) queue.Envelope[events.MovieRemovedEventV1] {
	return queue.NewEnvelope(events.MovieRemovedV1, events.Source, m.ID.String(), events.MovieRemovedEventV1{
		MovieID:   m.ID,
		LibraryID: m.LibraryID,
		FileID:    m.FileID,
	})
}

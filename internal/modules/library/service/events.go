package service

import (
	"context"
	"log/slog"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
)

// event is an event to publish: its type (the topic), subject and data.
type event struct {
	typ     string
	subject string
	data    any
}

// publish publishes the events in order, stopping at the first failure. The
// change they describe is already committed, so the publication is not
// canceled with the request, and a failure is only logged: ResyncLibrary
// republishes the state.
func (s *Service) publish(ctx context.Context, evs ...event) {
	ctx = context.WithoutCancel(ctx)
	for i := range evs {
		ev := &evs[i]
		msg, err := pubsub.Encode(pubsub.NewEnvelope(ev.typ, events.SourceLibrary, ev.subject, ev.data))
		if err == nil {
			err = s.pub.Publish(ctx, msg)
		}
		if err != nil {
			s.log.ErrorContext(ctx, "library: publish events failed, a resync reconciles the consumers",
				slog.String("type", ev.typ),
				slog.String("subject", ev.subject),
				slog.String("error", err.Error()),
			)
			return
		}
	}
}

func movieEvent(typ string, m *Movie) event {
	data := events.MovieEventV1{
		MovieID:    m.ID,
		LibraryID:  m.LibraryID,
		FileID:     m.FileID,
		LocationID: m.LocationID,
		Path:       m.Path,
		Title:      m.Title,
		Year:       m.Year,
	}
	if m.MediaInfo != nil {
		data.DurationMs = m.MediaInfo.DurationMs
	}
	return event{typ: typ, subject: m.ID.String(), data: data}
}

func movieRemovedEvent(m *Movie) event {
	return event{typ: events.MovieRemovedV1, subject: m.ID.String(), data: events.MovieRemovedEventV1{
		MovieID:   m.ID,
		LibraryID: m.LibraryID,
		FileID:    m.FileID,
	}}
}

func seriesEvent(typ string, sr *Series) event {
	return event{typ: typ, subject: sr.ID.String(), data: events.SeriesEventV1{
		SeriesID:  sr.ID,
		LibraryID: sr.LibraryID,
		Title:     sr.Title,
		Year:      sr.Year,
	}}
}

func seriesRemovedEvent(sr *Series) event {
	return event{typ: events.SeriesRemovedV1, subject: sr.ID.String(), data: events.SeriesRemovedEventV1{
		SeriesID:  sr.ID,
		LibraryID: sr.LibraryID,
	}}
}

func seasonEvent(typ string, sn *Season) event {
	return event{typ: typ, subject: sn.ID.String(), data: events.SeasonEventV1{
		SeasonID:     sn.ID,
		SeriesID:     sn.SeriesID,
		LibraryID:    sn.LibraryID,
		SeasonNumber: sn.Number,
	}}
}

// episodeEvent needs the series and season fields of e.
func episodeEvent(typ string, e *Episode) event {
	data := events.EpisodeEventV1{
		EpisodeID:        e.ID,
		LibraryID:        e.LibraryID,
		SeriesID:         e.SeriesID,
		SeriesTitle:      e.SeriesTitle,
		SeriesYear:       e.SeriesYear,
		SeasonID:         e.SeasonID,
		SeasonNumber:     e.SeasonNumber,
		EpisodeNumber:    e.Number,
		EpisodeEndNumber: e.EndNumber,
		Title:            e.Title,
		FileID:           e.FileID,
		LocationID:       e.LocationID,
		Path:             e.Path,
	}
	if e.MediaInfo != nil {
		data.DurationMs = e.MediaInfo.DurationMs
	}
	return event{typ: typ, subject: e.ID.String(), data: data}
}

func episodeRemovedEvent(e *Episode) event {
	return event{typ: events.EpisodeRemovedV1, subject: e.ID.String(), data: events.EpisodeRemovedEventV1{
		EpisodeID: e.ID,
		LibraryID: e.LibraryID,
		SeriesID:  e.SeriesID,
		SeasonID:  e.SeasonID,
		FileID:    e.FileID,
	}}
}

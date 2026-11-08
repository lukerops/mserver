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

func seriesEvent(typ string, sr Series) queue.Envelope[events.SeriesEventV1] {
	return queue.NewEnvelope(typ, events.Source, sr.ID.String(), events.SeriesEventV1{
		SeriesID:  sr.ID,
		LibraryID: sr.LibraryID,
		Title:     sr.Title,
		Year:      sr.Year,
	})
}

func seriesRemovedEvent(sr Series) queue.Envelope[events.SeriesRemovedEventV1] {
	return queue.NewEnvelope(events.SeriesRemovedV1, events.Source, sr.ID.String(), events.SeriesRemovedEventV1{
		SeriesID:  sr.ID,
		LibraryID: sr.LibraryID,
	})
}

func seasonEvent(typ string, sn Season) queue.Envelope[events.SeasonEventV1] {
	return queue.NewEnvelope(typ, events.Source, sn.ID.String(), events.SeasonEventV1{
		SeasonID:     sn.ID,
		SeriesID:     sn.SeriesID,
		LibraryID:    sn.LibraryID,
		SeasonNumber: sn.Number,
	})
}

// episodeEvent needs the series and season fields of e.
func episodeEvent(typ string, e Episode) queue.Envelope[events.EpisodeEventV1] {
	ev := events.EpisodeEventV1{
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
		ev.DurationMs = e.MediaInfo.DurationMs
	}
	return queue.NewEnvelope(typ, events.Source, e.ID.String(), ev)
}

func episodeRemovedEvent(e Episode) queue.Envelope[events.EpisodeRemovedEventV1] {
	return queue.NewEnvelope(events.EpisodeRemovedV1, events.Source, e.ID.String(), events.EpisodeRemovedEventV1{
		EpisodeID: e.ID,
		LibraryID: e.LibraryID,
		SeriesID:  e.SeriesID,
		SeasonID:  e.SeasonID,
		FileID:    e.FileID,
	})
}

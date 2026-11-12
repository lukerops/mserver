package service

import (
	"context"
	"log/slog"
	"path"
	"time"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/library/store"
)

// catalogMovie saves the movie of a file of a movie library. old is the
// movie of the file, or nil.
func (s *Service) catalogMovie(ctx context.Context, lib *Library, f *events.FileEventV1, old *Movie, contentChanged bool) error {
	parsed, ok := parseMovie(f.Path, time.Now().Year()+2)
	if !ok {
		// E.g. a video renamed to a trailer.
		s.log.DebugContext(ctx, "library: file ignored",
			slog.String("reason", "extra"),
			slog.String("library_id", lib.ID.String()),
			slog.String("file_id", f.FileID.String()),
			slog.String("path", f.Path),
		)
		return s.removeMovie(ctx, old)
	}

	now := time.Now().UTC()
	m := &Movie{
		ID:         movieID(f.FileID),
		LibraryID:  lib.ID,
		FileID:     f.FileID,
		LocationID: f.LocationID,
		Path:       f.Path,
		Title:      parsed.Title,
		SortTitle:  sortTitle(parsed.Title),
		Year:       parsed.Year,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	typ, msg := events.MovieAddedV1, "library: movie identified"
	if old != nil {
		m.CreatedAt = old.CreatedAt
		m.ExternalSubtitles = old.ExternalSubtitles
		if !contentChanged {
			m.MediaInfo = old.MediaInfo
		}
		if sameMovie(old, m) {
			s.log.DebugContext(ctx, "library: file ignored",
				slog.String("reason", "unchanged"),
				slog.String("movie_id", m.ID.String()),
				slog.String("file_id", f.FileID.String()),
				slog.String("path", f.Path),
			)
			return nil // a duplicate or a resync
		}
		typ, msg = events.MovieUpdatedV1, "library: movie updated"
	}
	if err := s.store.SaveMovie(ctx, m); err != nil {
		return err
	}
	s.log.DebugContext(ctx, msg,
		slog.String("movie_id", m.ID.String()),
		slog.String("title", m.Title),
		slog.Int("year", m.Year),
		slog.String("library_id", lib.ID.String()),
		slog.String("file_id", f.FileID.String()),
		slog.String("location_id", f.LocationID.String()),
		slog.String("path", f.Path),
		slog.Bool("media_info_kept", m.MediaInfo != nil),
	)
	s.publish(ctx, movieEvent(typ, m))
	return nil
}

// catalogEpisode saves the episode of a file of a series library, creating
// its series and season if needed. old is the episode of the file, or nil;
// its season and series are removed if the episode leaves them empty.
func (s *Service) catalogEpisode(ctx context.Context, lib *Library, f *events.FileEventV1, old *Episode, contentChanged bool) error {
	parsed, ok := parseEpisode(f.Path, time.Now().Year()+2)
	if !ok {
		reason := "no series name in the path"
		if _, main := mainVideo(f.Path); !main {
			reason = "extra"
		}
		s.log.DebugContext(ctx, "library: file ignored",
			slog.String("reason", reason),
			slog.String("library_id", lib.ID.String()),
			slog.String("file_id", f.FileID.String()),
			slog.String("path", f.Path),
		)
		return s.removeEpisode(ctx, old)
	}

	now := time.Now().UTC()
	// The store uses the candidates series and season only if the episode
	// does not join existing ones.
	series := &Series{
		ID:        newID(),
		LibraryID: lib.ID,
		Title:     parsed.SeriesTitle,
		SortTitle: sortTitle(parsed.SeriesTitle),
		Key:       seriesKey(parsed.SeriesTitle),
		Year:      parsed.SeriesYear,
		CreatedAt: now,
	}
	season := &Season{ID: newID(), Number: parsed.Season, CreatedAt: now}
	e := &Episode{
		ID:           episodeID(f.FileID),
		LibraryID:    lib.ID,
		FileID:       f.FileID,
		LocationID:   f.LocationID,
		Path:         f.Path,
		Title:        parsed.Title,
		SortKey:      naturalKey(path.Base(f.Path)),
		Number:       parsed.Number,
		EndNumber:    parsed.EndNumber,
		CreatedAt:    now,
		UpdatedAt:    now,
		SeriesTitle:  parsed.SeriesTitle,
		SeriesYear:   parsed.SeriesYear,
		SeasonNumber: parsed.Season,
	}
	typ, msg := events.EpisodeAddedV1, "library: episode identified"
	if old != nil {
		e.CreatedAt = old.CreatedAt
		e.ExternalSubtitles = old.ExternalSubtitles
		if !contentChanged {
			e.MediaInfo = old.MediaInfo
		}
		if sameEpisode(old, e) {
			s.log.DebugContext(ctx, "library: file ignored",
				slog.String("reason", "unchanged"),
				slog.String("episode_id", e.ID.String()),
				slog.String("file_id", f.FileID.String()),
				slog.String("path", f.Path),
			)
			return nil // a duplicate or a resync
		}
		typ, msg = events.EpisodeUpdatedV1, "library: episode updated"
	}

	changes, err := s.store.SaveEpisode(ctx, e, series, season)
	if err != nil {
		return err
	}
	s.log.DebugContext(ctx, msg,
		slog.String("episode_id", e.ID.String()),
		slog.String("title", e.Title),
		slog.String("series_id", e.SeriesID.String()),
		slog.String("series_title", e.SeriesTitle),
		slog.Int("series_year", e.SeriesYear),
		slog.Bool("new_series", changes.AddedSeries != nil),
		slog.String("season_id", e.SeasonID.String()),
		slog.Int("season_number", e.SeasonNumber),
		slog.Bool("new_season", changes.AddedSeason != nil),
		numberAttr("episode_number", e.Number),
		numberAttr("episode_end_number", e.EndNumber),
		slog.String("library_id", lib.ID.String()),
		slog.String("file_id", f.FileID.String()),
		slog.String("location_id", f.LocationID.String()),
		slog.String("path", f.Path),
		slog.Bool("media_info_kept", e.MediaInfo != nil),
	)

	// Additions go from the series down to the episode; removals, from the
	// season and series the episode left.
	var evs []event
	if changes.AddedSeries != nil {
		evs = append(evs, seriesEvent(events.SeriesAddedV1, changes.AddedSeries))
	}
	if changes.AddedSeason != nil {
		evs = append(evs, seasonEvent(events.SeasonAddedV1, changes.AddedSeason))
	}
	evs = append(evs, episodeEvent(typ, e))
	evs = s.appendRemoved(ctx, evs, changes)
	s.publish(ctx, evs...)
	return nil
}

// removeMovie deletes m, if it is not nil.
func (s *Service) removeMovie(ctx context.Context, m *Movie) error {
	if m == nil {
		return nil
	}
	if err := s.store.DeleteMovie(ctx, m.ID); err != nil {
		return err
	}
	s.log.DebugContext(ctx, "library: movie removed",
		slog.String("movie_id", m.ID.String()),
		slog.String("title", m.Title),
		slog.String("library_id", m.LibraryID.String()),
		slog.String("file_id", m.FileID.String()),
		slog.String("path", m.Path),
	)
	s.publish(ctx, movieRemovedEvent(m))
	return nil
}

// removeEpisode deletes e, if it is not nil, with its season and series if
// they are left empty.
func (s *Service) removeEpisode(ctx context.Context, e *Episode) error {
	if e == nil {
		return nil
	}
	changes, err := s.store.DeleteEpisode(ctx, e.ID)
	if err != nil {
		return err
	}
	s.log.DebugContext(ctx, "library: episode removed",
		slog.String("episode_id", e.ID.String()),
		slog.String("series_id", e.SeriesID.String()),
		slog.String("season_id", e.SeasonID.String()),
		slog.String("library_id", e.LibraryID.String()),
		slog.String("file_id", e.FileID.String()),
		slog.String("path", e.Path),
	)
	s.publish(ctx, s.appendRemoved(ctx, []event{episodeRemovedEvent(e)}, changes)...)
	return nil
}

// appendRemoved appends the events of the season and series removed along
// with an episode.
func (s *Service) appendRemoved(ctx context.Context, evs []event, changes *store.ShowChanges) []event {
	if sn := changes.RemovedSeason; sn != nil {
		s.log.DebugContext(ctx, "library: empty season removed",
			slog.String("season_id", sn.ID.String()),
			slog.String("series_id", sn.SeriesID.String()),
			slog.Int("season_number", sn.Number),
		)
		evs = append(evs, seasonEvent(events.SeasonRemovedV1, sn))
	}
	if sr := changes.RemovedSeries; sr != nil {
		s.log.DebugContext(ctx, "library: empty series removed",
			slog.String("series_id", sr.ID.String()),
			slog.String("title", sr.Title),
		)
		evs = append(evs, seriesRemovedEvent(sr))
	}
	return evs
}

// numberAttr is the attribute of an optional number for the logs.
func numberAttr(key string, n *int) slog.Attr {
	if n == nil {
		return slog.String(key, "unknown")
	}
	return slog.Int(key, *n)
}

// sameMovie reports whether a and b are the same, but for their update
// time.
func sameMovie(a, b *Movie) bool {
	return a.ID == b.ID && a.LibraryID == b.LibraryID && a.FileID == b.FileID &&
		a.LocationID == b.LocationID && a.Path == b.Path && a.Title == b.Title &&
		a.SortTitle == b.SortTitle && a.Year == b.Year && a.MediaInfo.Equal(b.MediaInfo)
}

// sameEpisode reports whether the stored episode old is the same as the
// parsed one e, but for the update time. The series is the same when it has
// the same key and year, even if the title is written differently, and so
// is the season when it has the same number.
func sameEpisode(old, e *Episode) bool {
	return old.ID == e.ID && old.LibraryID == e.LibraryID && old.FileID == e.FileID &&
		old.LocationID == e.LocationID && old.Path == e.Path && old.Title == e.Title &&
		old.SortKey == e.SortKey && sameInt(old.Number, e.Number) && sameInt(old.EndNumber, e.EndNumber) &&
		old.MediaInfo.Equal(e.MediaInfo) &&
		seriesKey(old.SeriesTitle) == seriesKey(e.SeriesTitle) &&
		old.SeriesYear == e.SeriesYear && old.SeasonNumber == e.SeasonNumber
}

func sameInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

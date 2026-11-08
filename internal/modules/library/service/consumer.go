package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/library/events"
	storageevents "github.com/lukerops/mserver/internal/modules/storage/events"
)

// StorageTopics are the topics HandleStorageEvent must be subscribed to.
var StorageTopics = []string{
	storageevents.FileIndexedV1,
	storageevents.FileChangedV1,
	storageevents.FileMovedV1,
	storageevents.FileRemovedV1,
	storageevents.FileAnalyzedV1,
}

func (s *service) HandleStorageEvent(ctx context.Context, msg queue.Message) error {
	switch msg.Topic {
	case storageevents.FileIndexedV1, storageevents.FileChangedV1, storageevents.FileMovedV1:
		var e queue.Envelope[storageevents.FileEventV1]
		if !s.decode(ctx, msg, &e) {
			return nil
		}
		return s.fileUpdated(ctx, e.Data, msg.Topic == storageevents.FileChangedV1)
	case storageevents.FileRemovedV1:
		var e queue.Envelope[storageevents.FileEventV1]
		if !s.decode(ctx, msg, &e) {
			return nil
		}
		return s.fileRemoved(ctx, e.Data.FileID)
	case storageevents.FileAnalyzedV1:
		var e queue.Envelope[storageevents.FileAnalyzedEventV1]
		if !s.decode(ctx, msg, &e) {
			return nil
		}
		return s.fileAnalyzed(ctx, e.Data)
	}
	return nil
}

// decode decodes the payload of msg. Invalid events are logged and
// dropped: delivering them again would not fix them.
func (s *service) decode(ctx context.Context, msg queue.Message, v any) bool {
	if err := json.Unmarshal(msg.Payload, v); err != nil {
		s.log.WarnContext(ctx, "library: invalid event dropped", "topic", msg.Topic, "error", err)
		return false
	}
	return true
}

// fileUpdated catalogs a file that was indexed, moved or changed, as a
// movie or as an episode depending on its library. A changed file loses
// its media info until it is analyzed again.
func (s *service) fileUpdated(ctx context.Context, f storageevents.FileEventV1, contentChanged bool) error {
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		oldMovie, movieExists, err := s.findMovie(ctx, f.FileID)
		if err != nil {
			return err
		}
		oldEpisode, episodeExists, err := s.findEpisode(ctx, f.FileID)
		if err != nil {
			return err
		}

		// A file is a movie or an episode, never both: the media type of a
		// library does not change, but a file may move between locations
		// of libraries of different types.
		lib, err := s.store.LibraryOfLocation(ctx, f.LocationID)
		switch {
		case errors.Is(err, ErrNotFound):
			if err := s.removeMovie(ctx, oldMovie, movieExists); err != nil {
				return err
			}
			return s.removeEpisode(ctx, oldEpisode, episodeExists)
		case err != nil:
			return err
		case lib.MediaType == MediaTypeTVShows:
			if err := s.removeMovie(ctx, oldMovie, movieExists); err != nil {
				return err
			}
			return s.catalogEpisode(ctx, lib, f, oldEpisode, episodeExists, contentChanged)
		default:
			if err := s.removeEpisode(ctx, oldEpisode, episodeExists); err != nil {
				return err
			}
			return s.catalogMovie(ctx, lib, f, oldMovie, movieExists, contentChanged)
		}
	})
}

// catalogMovie saves the movie of a file of a movie library. old is the
// movie of the file, if it exists.
func (s *service) catalogMovie(ctx context.Context, lib Library, f storageevents.FileEventV1, old Movie, exists, contentChanged bool) error {
	parsed, ok := parseMovie(f.Path, time.Now().Year()+2)
	if !ok {
		// E.g. a video renamed to something that is not a movie.
		return s.removeMovie(ctx, old, exists)
	}

	now := time.Now().UTC()
	m := Movie{
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
	event := events.MovieAddedV1
	if exists {
		m.CreatedAt = old.CreatedAt
		if !contentChanged {
			m.MediaInfo = old.MediaInfo
		}
		if sameMovie(old, m) {
			return nil // a duplicate or a resync
		}
		event = events.MovieUpdatedV1
	}
	if err := s.store.SaveMovie(ctx, m); err != nil {
		return err
	}
	return s.outbox.Publish(ctx, movieEvent(event, m))
}

// catalogEpisode saves the episode of a file of a series library,
// creating its series and season if needed. old is the episode of the
// file, if it exists; its season and series are removed if the episode
// leaves them empty.
func (s *service) catalogEpisode(ctx context.Context, lib Library, f storageevents.FileEventV1, old Episode, exists, contentChanged bool) error {
	parsed, ok := parseEpisode(f.Path, time.Now().Year()+2)
	if !ok {
		return s.removeEpisode(ctx, old, exists)
	}

	now := time.Now().UTC()
	var evs []queue.Event
	series, err := s.store.FindSeries(ctx, lib.ID, seriesKey(parsed.SeriesTitle), parsed.SeriesYear)
	if errors.Is(err, ErrNotFound) {
		series = Series{
			ID:        newID(),
			LibraryID: lib.ID,
			Title:     parsed.SeriesTitle,
			SortTitle: sortTitle(parsed.SeriesTitle),
			Key:       seriesKey(parsed.SeriesTitle),
			Year:      parsed.SeriesYear,
			CreatedAt: now,
		}
		if err := s.store.InsertSeries(ctx, series); err != nil {
			return err
		}
		evs = append(evs, seriesEvent(events.SeriesAddedV1, series))
	} else if err != nil {
		return err
	}
	season, err := s.store.FindSeason(ctx, series.ID, parsed.Season)
	if errors.Is(err, ErrNotFound) {
		season = Season{
			ID:        newID(),
			SeriesID:  series.ID,
			LibraryID: lib.ID,
			Number:    parsed.Season,
			CreatedAt: now,
		}
		if err := s.store.InsertSeason(ctx, season); err != nil {
			return err
		}
		evs = append(evs, seasonEvent(events.SeasonAddedV1, season))
	} else if err != nil {
		return err
	}

	e := Episode{
		ID:           episodeID(f.FileID),
		LibraryID:    lib.ID,
		SeriesID:     series.ID,
		SeasonID:     season.ID,
		FileID:       f.FileID,
		LocationID:   f.LocationID,
		Path:         f.Path,
		Title:        parsed.Title,
		SortKey:      naturalKey(path.Base(f.Path)),
		Number:       parsed.Number,
		EndNumber:    parsed.EndNumber,
		CreatedAt:    now,
		UpdatedAt:    now,
		SeriesTitle:  series.Title,
		SeriesYear:   series.Year,
		SeasonNumber: season.Number,
	}
	event := events.EpisodeAddedV1
	if exists {
		e.CreatedAt = old.CreatedAt
		if !contentChanged {
			e.MediaInfo = old.MediaInfo
		}
		if sameEpisode(old, e) {
			return nil // a duplicate or a resync
		}
		event = events.EpisodeUpdatedV1
	}
	if err := s.store.SaveEpisode(ctx, e); err != nil {
		return err
	}
	evs = append(evs, episodeEvent(event, e))

	if exists && old.SeasonID != e.SeasonID {
		pruned, err := s.prune(ctx, old.SeasonID, old.SeriesID)
		if err != nil {
			return err
		}
		evs = append(evs, pruned...)
	}
	return s.outbox.Publish(ctx, evs...)
}

func (s *service) fileRemoved(ctx context.Context, file uuid.UUID) error {
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		m, movieExists, err := s.findMovie(ctx, file)
		if err != nil {
			return err
		}
		if err := s.removeMovie(ctx, m, movieExists); err != nil {
			return err
		}
		e, episodeExists, err := s.findEpisode(ctx, file)
		if err != nil {
			return err
		}
		return s.removeEpisode(ctx, e, episodeExists)
	})
}

// findMovie returns the movie of a file and whether it exists.
func (s *service) findMovie(ctx context.Context, file uuid.UUID) (Movie, bool, error) {
	m, err := s.store.GetMovie(ctx, movieID(file))
	if errors.Is(err, ErrNotFound) {
		return Movie{}, false, nil
	}
	return m, err == nil, err
}

// findEpisode returns the episode of a file and whether it exists.
func (s *service) findEpisode(ctx context.Context, file uuid.UUID) (Episode, bool, error) {
	e, err := s.store.GetEpisode(ctx, episodeID(file))
	if errors.Is(err, ErrNotFound) {
		return Episode{}, false, nil
	}
	return e, err == nil, err
}

// removeMovie deletes m, if it exists.
func (s *service) removeMovie(ctx context.Context, m Movie, exists bool) error {
	if !exists {
		return nil
	}
	if err := s.store.DeleteMovie(ctx, m.ID); err != nil {
		return err
	}
	return s.outbox.Publish(ctx, movieRemovedEvent(m))
}

// removeEpisode deletes e, if it exists, with its season and series if
// they are left empty.
func (s *service) removeEpisode(ctx context.Context, e Episode, exists bool) error {
	if !exists {
		return nil
	}
	if err := s.store.DeleteEpisode(ctx, e.ID); err != nil {
		return err
	}
	// The outbox orders events by ID, given as they are created.
	removed := episodeRemovedEvent(e)
	pruned, err := s.prune(ctx, e.SeasonID, e.SeriesID)
	if err != nil {
		return err
	}
	return s.outbox.Publish(ctx, append([]queue.Event{removed}, pruned...)...)
}

// prune deletes a season and a series left without episodes, returning
// their removed events.
func (s *service) prune(ctx context.Context, seasonID, seriesID uuid.UUID) ([]queue.Event, error) {
	var evs []queue.Event
	season, err := s.store.GetSeason(ctx, seasonID)
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return nil, err
	case season.EpisodeCount == 0:
		if err := s.store.DeleteSeason(ctx, season.ID); err != nil {
			return nil, err
		}
		evs = append(evs, seasonEvent(events.SeasonRemovedV1, season))
	}
	series, err := s.store.GetSeries(ctx, seriesID)
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return nil, err
	case series.EpisodeCount == 0:
		// Its seasons are empty, and were removed with their last episode.
		if err := s.store.DeleteSeries(ctx, series.ID); err != nil {
			return nil, err
		}
		evs = append(evs, seriesRemovedEvent(series))
	}
	return evs, nil
}

// fileAnalyzed saves the media info of the movie or episode of a file,
// from the results of ffprobe version 1. Other analyses and files that are
// not cataloged are ignored.
func (s *service) fileAnalyzed(ctx context.Context, a storageevents.FileAnalyzedEventV1) error {
	if a.Analyzer != storageevents.AnalyzerFFprobe {
		return nil
	}
	if a.Version != 1 {
		s.log.WarnContext(ctx, "library: unsupported analysis version",
			"analyzer", a.Analyzer, "version", a.Version, "file", a.FileID)
		return nil
	}
	var result storageevents.FFprobeResultV1
	if err := json.Unmarshal(a.Result, &result); err != nil {
		s.log.WarnContext(ctx, "library: invalid analysis dropped",
			"analyzer", a.Analyzer, "file", a.FileID, "error", err)
		return nil
	}
	info := mediaInfo(result)

	return s.store.WithTx(ctx, func(ctx context.Context) error {
		m, exists, err := s.findMovie(ctx, a.FileID)
		if err != nil {
			return err
		}
		if exists && !reflect.DeepEqual(m.MediaInfo, info) {
			m.MediaInfo = info
			m.UpdatedAt = time.Now().UTC()
			if err := s.store.SaveMovie(ctx, m); err != nil {
				return err
			}
			if err := s.outbox.Publish(ctx, movieEvent(events.MovieUpdatedV1, m)); err != nil {
				return err
			}
		}

		e, exists, err := s.findEpisode(ctx, a.FileID)
		if err != nil {
			return err
		}
		if !exists || reflect.DeepEqual(e.MediaInfo, info) {
			return nil
		}
		e.MediaInfo = info
		e.UpdatedAt = time.Now().UTC()
		if err := s.store.SaveEpisode(ctx, e); err != nil {
			return err
		}
		return s.outbox.Publish(ctx, episodeEvent(events.EpisodeUpdatedV1, e))
	})
}

// mediaInfo converts an ffprobe result to the media info of the catalog.
func mediaInfo(r storageevents.FFprobeResultV1) *MediaInfo {
	info := &MediaInfo{
		Container:  r.Container,
		DurationMs: r.DurationMs,
		BitRate:    r.BitRate,
		Size:       r.Size,
		Streams:    make([]Stream, len(r.Streams)),
	}
	for i, s := range r.Streams {
		// Same fields: the catalog keeps everything ffprobe reports.
		info.Streams[i] = Stream(s)
	}
	return info
}

// sameMovie reports whether a and b differ only in their update time.
func sameMovie(a, b Movie) bool {
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

// sameEpisode reports whether a and b differ only in their update time.
func sameEpisode(a, b Episode) bool {
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

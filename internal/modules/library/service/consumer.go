package service

import (
	"context"
	"encoding/json"
	"errors"
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

// fileUpdated catalogs a file that was indexed, moved or changed. A
// changed file loses its media info until it is analyzed again.
func (s *service) fileUpdated(ctx context.Context, f storageevents.FileEventV1, contentChanged bool) error {
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		id := movieID(f.FileID)
		old, err := s.store.GetMovie(ctx, id)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}

		lib, err := s.store.LibraryOfLocation(ctx, f.LocationID)
		if errors.Is(err, ErrNotFound) {
			return s.removeMovie(ctx, old, exists)
		}
		if err != nil {
			return err
		}
		parsed, ok := parseMovie(f.Path, time.Now().Year()+2)
		if !ok || lib.MediaType != MediaTypeMovies {
			// E.g. a video renamed to something that is not a movie.
			return s.removeMovie(ctx, old, exists)
		}

		now := time.Now().UTC()
		m := Movie{
			ID:         id,
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
	})
}

func (s *service) fileRemoved(ctx context.Context, file uuid.UUID) error {
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		m, err := s.store.GetMovie(ctx, movieID(file))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.removeMovie(ctx, m, true)
	})
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

// fileAnalyzed saves the media info of the movie of a file, from the
// results of ffprobe version 1. Other analyses and files that are not
// movies are ignored.
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
		m, err := s.store.GetMovie(ctx, movieID(a.FileID))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if reflect.DeepEqual(m.MediaInfo, info) {
			return nil
		}
		m.MediaInfo = info
		m.UpdatedAt = time.Now().UTC()
		if err := s.store.SaveMovie(ctx, m); err != nil {
			return err
		}
		return s.outbox.Publish(ctx, movieEvent(events.MovieUpdatedV1, m))
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

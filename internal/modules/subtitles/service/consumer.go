package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/subtitles/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// StorageTopics are the topics HandleStorageEvent must be subscribed to.
var StorageTopics = []string{
	events.FileIndexedV1,
	events.FileChangedV1,
	events.FileMovedV1,
	events.FileRemovedV1,
}

// LibraryTopics are the topics HandleLibraryEvent must be subscribed to.
var LibraryTopics = []string{
	events.MovieAddedV1,
	events.MovieUpdatedV1,
	events.MovieRemovedV1,
	events.EpisodeAddedV1,
	events.EpisodeUpdatedV1,
	events.EpisodeRemovedV1,
}

// HandleStorageEvent updates the subtitle files with an event of the
// storage (one of StorageTopics). It is idempotent.
func (s *Service) HandleStorageEvent(ctx context.Context, msg pubsub.Message) error {
	e, err := pubsub.Decode[events.FileEventV1](msg)
	if err != nil {
		return s.dropInvalid(ctx, msg, err)
	}
	if msg.Topic == events.FileRemovedV1 {
		return s.fileRemoved(ctx, e.Data.FileID)
	}
	return s.fileUpdated(ctx, &e.Data)
}

// HandleLibraryEvent updates the items with an event of the library (one of
// LibraryTopics). It is idempotent.
func (s *Service) HandleLibraryEvent(ctx context.Context, msg pubsub.Message) error {
	switch msg.Topic {
	case events.MovieAddedV1, events.MovieUpdatedV1:
		e, err := pubsub.Decode[events.MovieEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		return s.itemUpdated(ctx, &Item{
			ID: e.Data.MovieID, Type: events.ItemTypeMovie, FileID: e.Data.FileID,
			LocationID: e.Data.LocationID, Path: e.Data.Path,
		})
	case events.EpisodeAddedV1, events.EpisodeUpdatedV1:
		e, err := pubsub.Decode[events.EpisodeEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		return s.itemUpdated(ctx, &Item{
			ID: e.Data.EpisodeID, Type: events.ItemTypeEpisode, FileID: e.Data.FileID,
			LocationID: e.Data.LocationID, Path: e.Data.Path,
		})
	case events.MovieRemovedV1:
		e, err := pubsub.Decode[events.MovieRemovedEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		return s.itemRemoved(ctx, e.Data.MovieID)
	case events.EpisodeRemovedV1:
		e, err := pubsub.Decode[events.EpisodeRemovedEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		return s.itemRemoved(ctx, e.Data.EpisodeID)
	}
	return nil
}

// dropInvalid logs an invalid event and drops it: delivering it again
// would not fix it.
func (s *Service) dropInvalid(ctx context.Context, msg pubsub.Message, err error) error {
	s.log.WarnContext(ctx, "subtitles: invalid event dropped",
		slog.String("topic", msg.Topic),
		slog.String("error", err.Error()),
	)
	return nil
}

// fileUpdated records a file that was indexed, moved or changed when it is
// a subtitle (by its media type). A file that is not a subtitle is ignored,
// and forgotten if it was one before (e.g. a subtitle renamed to a text).
func (s *Service) fileUpdated(ctx context.Context, f *events.FileEventV1) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, err := s.store.GetSubtitleFile(ctx, f.FileID)
	if errors.Is(err, store.ErrNotFound) {
		old, err = nil, nil
	}
	if err != nil {
		return err
	}

	format, ok := subtitleFormat(f.MimeType, f.Path)
	if !ok {
		if old == nil {
			return nil
		}
		return s.forgetFile(ctx, old)
	}

	file := &SubtitleFile{
		FileID:     f.FileID,
		LocationID: f.LocationID,
		Path:       f.Path,
		Dir:        dirOf(f.LocationID, f.Path).dir,
		Format:     format,
	}
	if err := s.store.SaveSubtitleFile(ctx, file); err != nil {
		return err
	}
	s.log.DebugContext(ctx, "subtitles: subtitle file recorded",
		slog.String("file_id", f.FileID.String()),
		slog.String("location_id", f.LocationID.String()),
		slog.String("path", f.Path),
		slog.String("format", format),
	)

	dirs := []dirKey{dirOf(f.LocationID, f.Path)}
	if old != nil {
		// A file that moved leaves matches behind in its old directory.
		dirs = append(dirs, dirKey{old.LocationID, old.Dir})
	}
	return s.reconcile(ctx, dirs...)
}

func (s *Service) fileRemoved(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, err := s.store.GetSubtitleFile(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.forgetFile(ctx, old)
}

// forgetFile removes a subtitle file and its matches. The caller holds
// s.mu.
func (s *Service) forgetFile(ctx context.Context, f *SubtitleFile) error {
	if err := s.store.DeleteSubtitleFile(ctx, f.FileID); err != nil {
		return err
	}
	s.log.DebugContext(ctx, "subtitles: subtitle file forgotten",
		slog.String("file_id", f.FileID.String()),
		slog.String("path", f.Path),
	)
	return s.reconcile(ctx, dirKey{f.LocationID, f.Dir})
}

// itemUpdated records a movie or episode that was added or updated.
func (s *Service) itemUpdated(ctx context.Context, it *Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, err := s.store.GetItem(ctx, it.ID)
	if errors.Is(err, store.ErrNotFound) {
		old, err = nil, nil
	}
	if err != nil {
		return err
	}

	it.Dir = dirOf(it.LocationID, it.Path).dir
	if err := s.store.SaveItem(ctx, it); err != nil {
		return err
	}
	s.log.DebugContext(ctx, "subtitles: item recorded",
		slog.String("item_id", it.ID.String()),
		slog.String("item_type", it.Type),
		slog.String("location_id", it.LocationID.String()),
		slog.String("path", it.Path),
	)

	dirs := []dirKey{{it.LocationID, it.Dir}}
	if old != nil {
		dirs = append(dirs, dirKey{old.LocationID, old.Dir})
	}
	return s.reconcile(ctx, dirs...)
}

func (s *Service) itemRemoved(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, err := s.store.GetItem(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.store.DeleteItem(ctx, id); err != nil {
		return err
	}
	s.log.DebugContext(ctx, "subtitles: item forgotten",
		slog.String("item_id", id.String()),
		slog.String("path", old.Path),
	)
	return s.reconcile(ctx, dirKey{old.LocationID, old.Dir})
}

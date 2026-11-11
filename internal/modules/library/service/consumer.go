package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
)

// StorageTopics are the topics HandleStorageEvent must be subscribed to.
var StorageTopics = []string{
	events.FileIndexedV1,
	events.FileChangedV1,
	events.FileMovedV1,
	events.FileRemovedV1,
	events.FileAnalyzedV1,
}

// HandleStorageEvent updates the catalog with an event of the storage (one
// of StorageTopics). It is idempotent.
func (s *Service) HandleStorageEvent(ctx context.Context, msg pubsub.Message) error {
	switch msg.Topic {
	case events.FileIndexedV1, events.FileChangedV1, events.FileMovedV1:
		e, err := pubsub.Decode[events.FileEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		return s.fileUpdated(ctx, &e.Data, msg.Topic == events.FileChangedV1)
	case events.FileRemovedV1:
		e, err := pubsub.Decode[events.FileEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		return s.fileRemoved(ctx, e.Data.FileID)
	case events.FileAnalyzedV1:
		e, err := pubsub.Decode[events.FileAnalyzedEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		return s.fileAnalyzed(ctx, &e.Data)
	}
	return nil
}

// dropInvalid logs an invalid event and drops it: delivering it again
// would not fix it.
func (s *Service) dropInvalid(ctx context.Context, msg pubsub.Message, err error) error {
	s.log.WarnContext(ctx, "library: invalid event dropped",
		slog.String("topic", msg.Topic),
		slog.String("error", err.Error()),
	)
	return nil
}

// fileUpdated catalogs a file that was indexed, moved or changed, as a
// movie or as an episode depending on its library. A changed file loses
// its media info until it is analyzed again. Files that are not videos
// (by their media type) are ignored, and are removed from the catalog if
// they were cataloged before (e.g. a video renamed to something else).
func (s *Service) fileUpdated(ctx context.Context, f *events.FileEventV1, contentChanged bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldMovie, err := s.findMovie(ctx, f.FileID)
	if err != nil {
		return err
	}
	oldEpisode, err := s.findEpisode(ctx, f.FileID)
	if err != nil {
		return err
	}

	if !isVideo(f.MimeType) {
		s.log.DebugContext(ctx, "library: file ignored",
			slog.String("reason", "not a video"),
			slog.String("file_id", f.FileID.String()),
			slog.String("mime_type", f.MimeType),
			slog.String("path", f.Path),
		)
		if err := s.removeMovie(ctx, oldMovie); err != nil {
			return err
		}
		return s.removeEpisode(ctx, oldEpisode)
	}

	// A file is a movie or an episode, never both: the media type of a
	// library does not change, but a file may move between locations of
	// libraries of different types.
	lib, err := s.store.LibraryOfLocation(ctx, f.LocationID)
	switch {
	case errors.Is(err, ErrNotFound):
		s.log.DebugContext(ctx, "library: file ignored",
			slog.String("reason", "no library on the location"),
			slog.String("file_id", f.FileID.String()),
			slog.String("location_id", f.LocationID.String()),
			slog.String("path", f.Path),
		)
		if err := s.removeMovie(ctx, oldMovie); err != nil {
			return err
		}
		return s.removeEpisode(ctx, oldEpisode)
	case err != nil:
		return err
	case lib.MediaType == MediaTypeTVShows:
		if err := s.removeMovie(ctx, oldMovie); err != nil {
			return err
		}
		return s.catalogEpisode(ctx, lib, f, oldEpisode, contentChanged)
	default:
		if err := s.removeEpisode(ctx, oldEpisode); err != nil {
			return err
		}
		return s.catalogMovie(ctx, lib, f, oldMovie, contentChanged)
	}
}

func (s *Service) fileRemoved(ctx context.Context, file uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.findMovie(ctx, file)
	if err != nil {
		return err
	}
	e, err := s.findEpisode(ctx, file)
	if err != nil {
		return err
	}
	if m == nil && e == nil {
		s.log.DebugContext(ctx, "library: file ignored",
			slog.String("reason", "not cataloged"),
			slog.String("file_id", file.String()),
		)
		return nil
	}
	if err := s.removeMovie(ctx, m); err != nil {
		return err
	}
	return s.removeEpisode(ctx, e)
}

// isVideo reports whether a media type, as set by the storage, is a video.
func isVideo(mimeType string) bool {
	return strings.HasPrefix(mimeType, "video/")
}

// findMovie returns the movie of a file, or nil if there is none.
func (s *Service) findMovie(ctx context.Context, file uuid.UUID) (*Movie, error) {
	m, err := s.store.GetMovie(ctx, movieID(file))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return m, err
}

// findEpisode returns the episode of a file, or nil if there is none.
func (s *Service) findEpisode(ctx context.Context, file uuid.UUID) (*Episode, error) {
	e, err := s.store.GetEpisode(ctx, episodeID(file))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return e, err
}

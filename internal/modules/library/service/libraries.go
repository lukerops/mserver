package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/internal/events"
)

// CreateLibrary registers a library and asks the storage to resync its
// locations, so that their files are cataloged. A location belongs to at
// most one library. A failed resync is retried a few times and then only
// logged: the library is created anyway.
func (s *Service) CreateLibrary(ctx context.Context, req *CreateLibraryRequest) (*Library, error) {
	lib := &Library{
		ID:          newID(),
		Name:        strings.TrimSpace(req.Name),
		MediaType:   req.MediaType,
		LocationIDs: req.LocationIDs,
		CreatedAt:   time.Now().UTC(),
	}
	switch {
	case lib.Name == "":
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	case lib.MediaType != MediaTypeMovies && lib.MediaType != MediaTypeTVShows:
		return nil, fmt.Errorf("%w: unsupported media type %q", ErrInvalidArgument, lib.MediaType)
	case len(lib.LocationIDs) == 0:
		return nil, fmt.Errorf("%w: at least one location is required", ErrInvalidArgument)
	}
	for i, loc := range lib.LocationIDs {
		if loc == uuid.Nil || slices.Contains(lib.LocationIDs[:i], loc) {
			return nil, fmt.Errorf("%w: invalid or repeated location %s", ErrInvalidArgument, loc)
		}
	}

	for _, loc := range lib.LocationIDs {
		_, err := s.storage.GetLocation(ctx, &storagev1.GetLocationRequest{Id: loc.String()})
		if err != nil && connect.CodeOf(err) == connect.CodeNotFound {
			return nil, fmt.Errorf("%w: unknown location %s", ErrInvalidArgument, loc)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: get location %s: %w", ErrUnavailable, loc, err)
		}
	}

	if err := s.store.InsertLibrary(ctx, lib); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "library: library created",
		slog.String("library_id", lib.ID.String()),
		slog.String("name", lib.Name),
		slog.String("media_type", lib.MediaType),
		slog.String("location_ids", joinIDs(lib.LocationIDs)),
	)

	// The files of the locations are cataloged from the events of the
	// resync. A failed resync is not fatal: the library exists, and the
	// files are cataloged as the storage finds changes in them.
	for _, loc := range lib.LocationIDs {
		if err := s.resyncLocation(ctx, loc); err != nil {
			s.log.WarnContext(ctx, "library: resync of location failed",
				slog.String("library_id", lib.ID.String()),
				slog.String("location_id", loc.String()),
				slog.String("error", err.Error()),
			)
		} else {
			s.log.InfoContext(ctx, "library: resync of location requested",
				slog.String("library_id", lib.ID.String()),
				slog.String("location_id", loc.String()),
			)
		}
	}
	return lib, nil
}

// resyncLocation asks the storage to resync a location, retrying after
// each wait of resyncBackoff.
func (s *Service) resyncLocation(ctx context.Context, loc uuid.UUID) error {
	resync := func() error {
		_, err := s.storage.ResyncLocation(ctx, &storagev1.ResyncLocationRequest{Id: loc.String()})
		return err
	}
	err := resync()
	for _, wait := range s.resyncBackoff {
		if err == nil {
			return nil
		}
		s.log.DebugContext(ctx, "library: resync of location failed, retrying",
			slog.String("location_id", loc.String()),
			slog.Int64("wait", int64(wait)),
			slog.String("error", err.Error()),
		)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
		err = resync()
	}
	return err
}

func (s *Service) GetLibrary(ctx context.Context, id uuid.UUID) (*Library, error) {
	return s.store.GetLibrary(ctx, id)
}

// ListLibraries lists the libraries ordered by name.
func (s *Service) ListLibraries(ctx context.Context) ([]Library, error) {
	return s.store.ListLibraries(ctx)
}

// DeleteLibrary removes a library with its movies, series, seasons and
// episodes, publishing their removed events. Its locations can then join
// another library.
func (s *Service) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lib, err := s.store.GetLibrary(ctx, id)
	if err != nil {
		return err
	}
	movies, _, err := s.store.ListMovies(ctx, &MovieQuery{LibraryID: id})
	if err != nil {
		return err
	}
	series, seasons, episodes, err := s.listShows(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteLibrary(ctx, id); err != nil {
		return err
	}

	evs := make([]event, 0, len(movies)+len(episodes)+len(seasons)+len(series))
	for i := range movies {
		evs = append(evs, movieRemovedEvent(&movies[i]))
	}
	// Removals go from the episodes up to their series.
	for i := range episodes {
		evs = append(evs, episodeRemovedEvent(&episodes[i]))
	}
	for i := range seasons {
		evs = append(evs, seasonEvent(events.SeasonRemovedV1, &seasons[i]))
	}
	for i := range series {
		evs = append(evs, seriesRemovedEvent(&series[i]))
	}
	s.publish(ctx, evs...)

	s.log.InfoContext(ctx, "library: library deleted",
		slog.String("library_id", lib.ID.String()),
		slog.String("name", lib.Name),
		slog.String("media_type", lib.MediaType),
		slog.String("location_ids", joinIDs(lib.LocationIDs)),
		slog.Int("movies", len(movies)),
		slog.Int("series", len(series)),
		slog.Int("seasons", len(seasons)),
		slog.Int("episodes", len(episodes)),
	)
	return nil
}

// ResyncLibrary publishes again the added event of every movie, series,
// season and episode of the library, so that consumers can rebuild their
// projections.
func (s *Service) ResyncLibrary(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.store.GetLibrary(ctx, id); err != nil {
		return err
	}
	movies, _, err := s.store.ListMovies(ctx, &MovieQuery{LibraryID: id})
	if err != nil {
		return err
	}
	series, seasons, episodes, err := s.listShows(ctx, id)
	if err != nil {
		return err
	}

	evs := make([]event, 0, len(movies)+len(series)+len(seasons)+len(episodes))
	for i := range movies {
		evs = append(evs, movieEvent(events.MovieAddedV1, &movies[i]))
	}
	// Additions go from the series down to their episodes.
	for i := range series {
		evs = append(evs, seriesEvent(events.SeriesAddedV1, &series[i]))
	}
	for i := range seasons {
		evs = append(evs, seasonEvent(events.SeasonAddedV1, &seasons[i]))
	}
	for i := range episodes {
		evs = append(evs, episodeEvent(events.EpisodeAddedV1, &episodes[i]))
	}
	s.publish(ctx, evs...)

	s.log.InfoContext(ctx, "library: library resynced",
		slog.String("library_id", id.String()),
		slog.Int("movies", len(movies)),
		slog.Int("series", len(series)),
		slog.Int("seasons", len(seasons)),
		slog.Int("episodes", len(episodes)),
	)
	return nil
}

// listShows lists the series, seasons and episodes of a library.
func (s *Service) listShows(ctx context.Context, library uuid.UUID) ([]Series, []Season, []Episode, error) {
	series, _, err := s.store.ListSeries(ctx, &SeriesQuery{LibraryID: library})
	if err != nil {
		return nil, nil, nil, err
	}
	seasons, err := s.store.ListSeasons(ctx, &SeasonQuery{LibraryID: library})
	if err != nil {
		return nil, nil, nil, err
	}
	episodes, _, err := s.store.ListEpisodes(ctx, &EpisodeQuery{LibraryID: library})
	if err != nil {
		return nil, nil, nil, err
	}
	return series, seasons, episodes, nil
}

// joinIDs joins the IDs with commas, for the logs.
func joinIDs(ids []uuid.UUID) string {
	s := make([]string, len(ids))
	for i := range ids {
		s[i] = ids[i].String()
	}
	return strings.Join(s, ",")
}

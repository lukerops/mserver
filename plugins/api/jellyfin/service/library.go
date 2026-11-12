package service

import (
	"context"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

// maxLibraryPage is the largest page the library module serves.
const maxLibraryPage = 1000

// itemRef is an item of the catalog: exactly one field is set, unless the
// item does not exist.
type itemRef struct {
	movie   *libraryv1.Movie
	episode *libraryv1.Episode
	series  *libraryv1.Series
	season  *libraryv1.Season
	library *libraryv1.Library
}

func (ref itemRef) exists() bool {
	return ref.movie != nil || ref.episode != nil || ref.series != nil || ref.season != nil || ref.library != nil
}

// isFolder reports whether the item has children /Items can list.
func (ref itemRef) isFolder() bool {
	return ref.library != nil || ref.series != nil || ref.season != nil
}

// Libraries lists the libraries ("My Media") as collection folders.
func (s *Service) Libraries(ctx context.Context) ([]Item, error) {
	items := []Item{}
	if s.library == nil {
		return items, nil
	}
	res, err := s.library.ListLibraries(ctx, &libraryv1.ListLibrariesRequest{})
	if err != nil {
		return nil, err
	}
	for _, l := range res.GetLibraries() {
		items = append(items, libraryItem(l))
	}
	return items, nil
}

// Item finds an item with an id, which may be of any type, with its media
// sources.
func (s *Service) Item(ctx context.Context, id uuid.UUID) (Item, error) {
	if s.library == nil {
		return Item{}, errNotFound("item not found: no library module")
	}
	ref, err := s.findItem(ctx, id)
	if err != nil {
		return Item{}, err
	}
	switch {
	case ref.movie != nil:
		return movieItem(ref.movie, true), nil
	case ref.episode != nil:
		return episodeItem(ref.episode, true), nil
	case ref.series != nil:
		return seriesItem(ref.series), nil
	case ref.season != nil:
		return seasonItem(ref.season), nil
	case ref.library != nil:
		return libraryItem(ref.library), nil
	}
	return Item{}, errNotFound("item not found")
}

// Video finds a playable item (a movie or an episode), with the file the
// storage serves and its external subtitles.
func (s *Service) Video(ctx context.Context, id uuid.UUID) (Video, error) {
	if s.library == nil {
		return Video{}, errNotFound("item not found: no library module")
	}
	ref, err := s.findItem(ctx, id)
	if err != nil {
		return Video{}, err
	}
	switch {
	case ref.movie != nil:
		m := ref.movie
		return Video{
			Item:         movieItem(m, true),
			FileID:       m.GetFileId(),
			Subtitles:    externalSubtitles(m.GetExternalSubtitles()),
			SubtitleBase: externalSubtitleBase(m.GetMediaInfo()),
		}, nil
	case ref.episode != nil:
		e := ref.episode
		return Video{
			Item:         episodeItem(e, true),
			FileID:       e.GetFileId(),
			Subtitles:    externalSubtitles(e.GetExternalSubtitles()),
			SubtitleBase: externalSubtitleBase(e.GetMediaInfo()),
		}, nil
	}
	return Video{}, errNotFound("item is not a movie or an episode")
}

// findItem finds the item with an id, which may be of any type. A missing
// item is not an error: the returned itemRef does not exist.
func (s *Service) findItem(ctx context.Context, id uuid.UUID) (itemRef, error) {
	key := id.String()
	if res, err := s.library.GetMovie(ctx, &libraryv1.GetMovieRequest{Id: key}); err == nil {
		return itemRef{movie: res.GetMovie()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := s.library.GetEpisode(ctx, &libraryv1.GetEpisodeRequest{Id: key}); err == nil {
		return itemRef{episode: res.GetEpisode()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := s.library.GetSeries(ctx, &libraryv1.GetSeriesRequest{Id: key}); err == nil {
		return itemRef{series: res.GetSeries()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := s.library.GetSeason(ctx, &libraryv1.GetSeasonRequest{Id: key}); err == nil {
		return itemRef{season: res.GetSeason()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := s.library.GetLibrary(ctx, &libraryv1.GetLibraryRequest{Id: key}); err == nil {
		return itemRef{library: res.GetLibrary()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	return itemRef{}, nil
}

// parentScope resolves a parentId. It returns false if the parent is not a
// folder (e.g. a movie, or a missing item): it has no children.
func (s *Service) parentScope(ctx context.Context, parent string) (itemRef, bool, error) {
	id, err := uuid.Parse(parent)
	if err != nil {
		return itemRef{}, false, errInvalidArgument("invalid parentId")
	}
	ref, err := s.findItem(ctx, id)
	if err != nil {
		return itemRef{}, false, err
	}
	return ref, ref.isFolder(), nil
}

// Seasons lists the seasons of a series by number, the specials first.
func (s *Service) Seasons(ctx context.Context, seriesID uuid.UUID) ([]Item, error) {
	if s.library == nil {
		return nil, errNotFound("series not found: no library module")
	}
	res, err := s.library.GetSeries(ctx, &libraryv1.GetSeriesRequest{Id: seriesID.String()})
	if isCode(err, connect.CodeNotFound) {
		return nil, errNotFound("series not found")
	}
	if err != nil {
		return nil, err
	}
	seasons, err := s.listSeasons(ctx, res.GetSeries())
	if err != nil {
		return nil, err
	}
	items := make([]Item, len(seasons))
	for i, season := range seasons {
		items[i] = seasonItem(season)
	}
	return items, nil
}

// listSeasons lists the seasons of a series. A series removed meanwhile has
// no seasons.
func (s *Service) listSeasons(ctx context.Context, series *libraryv1.Series) ([]*libraryv1.Season, error) {
	res, err := s.library.ListSeasons(ctx, &libraryv1.ListSeasonsRequest{
		LibraryId: series.GetLibraryId(),
		SeriesId:  series.GetId(),
	})
	if isCode(err, connect.CodeNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return res.GetSeasons(), nil
}

// Episodes lists the episodes of a series, or of one of its seasons (by
// seasonId or by season number), in order. StartItemID starts the list at
// an episode, before Start and Limit apply.
func (s *Service) Episodes(ctx context.Context, q EpisodeQuery) (ItemQueryResult, error) {
	if s.library == nil {
		return ItemQueryResult{}, errNotFound("series not found: no library module")
	}
	res, err := s.library.GetSeries(ctx, &libraryv1.GetSeriesRequest{Id: q.SeriesID.String()})
	if isCode(err, connect.CodeNotFound) {
		return ItemQueryResult{}, errNotFound("series not found")
	}
	if err != nil {
		return ItemQueryResult{}, err
	}
	series := res.GetSeries()
	empty := ItemQueryResult{Items: []Item{}, StartIndex: q.Start}

	var seasonID string
	switch {
	case q.SeasonID != "":
		seasonID = q.SeasonID
	case q.SeasonNumber != nil:
		seasons, err := s.listSeasons(ctx, series)
		if err != nil {
			return ItemQueryResult{}, err
		}
		for _, season := range seasons {
			if int(season.GetNumber()) == *q.SeasonNumber {
				seasonID = season.GetId()
			}
		}
		if seasonID == "" {
			return empty, nil
		}
	}

	episodes, _, err := s.listEpisodes(ctx, itemQuery{
		libraryID: series.GetLibraryId(),
		seriesID:  series.GetId(),
		seasonID:  seasonID,
	}, 0, 0)
	if isCode(err, connect.CodeNotFound) {
		// The season is not of the series, or was removed meanwhile.
		return empty, nil
	}
	if err != nil {
		return ItemQueryResult{}, err
	}
	if q.StartItemID != "" {
		for i, e := range episodes {
			if e.GetId() == q.StartItemID {
				episodes = episodes[i:]
				break
			}
		}
	}

	result := ItemQueryResult{Items: []Item{}, TotalRecordCount: len(episodes), StartIndex: q.Start}
	for _, e := range paginate(episodes, q.Start, q.Limit) {
		result.Items = append(result.Items, episodeItem(e, q.Full))
	}
	return result, nil
}

// listPaged collects up to limit items (all of them when limit is zero)
// from offset on, asking list for pages of at most maxLibraryPage items.
// It also returns the total number of items, ignoring offset and limit.
func listPaged[T any](offset, limit int, list func(offset, limit int) ([]T, int, error)) ([]T, int, error) {
	var out []T
	total := 0
	for {
		n := maxLibraryPage
		if limit > 0 && limit-len(out) < n {
			n = limit - len(out)
		}
		page, pageTotal, err := list(offset+len(out), n)
		if err != nil {
			return nil, 0, err
		}
		total = pageTotal
		out = append(out, page...)
		if len(page) < n || (limit > 0 && len(out) >= limit) || offset+len(out) >= total {
			return out, total, nil
		}
	}
}

// listMovies lists the movies matching the query.
func (s *Service) listMovies(ctx context.Context, iq itemQuery, offset, limit int) ([]*libraryv1.Movie, int, error) {
	return listPaged(offset, limit, func(offset, limit int) ([]*libraryv1.Movie, int, error) {
		res, err := s.library.ListMovies(ctx, &libraryv1.ListMoviesRequest{
			LibraryId:         iq.libraryID,
			Ids:               iq.ids,
			Years:             iq.years,
			AudioLanguages:    iq.audio,
			SubtitleLanguages: iq.subtitle,
			SortBy:            iq.sortBy,
			Descending:        iq.descending,
			Limit:             int32(limit),
			Offset:            int32(offset),
		})
		if err != nil {
			return nil, 0, err
		}
		return res.GetMovies(), int(res.GetTotal()), nil
	})
}

// listSeries lists the series matching the query.
func (s *Service) listSeries(ctx context.Context, iq itemQuery, offset, limit int) ([]*libraryv1.Series, int, error) {
	return listPaged(offset, limit, func(offset, limit int) ([]*libraryv1.Series, int, error) {
		res, err := s.library.ListSeries(ctx, &libraryv1.ListSeriesRequest{
			LibraryId:         iq.libraryID,
			Ids:               iq.ids,
			Years:             iq.years,
			AudioLanguages:    iq.audio,
			SubtitleLanguages: iq.subtitle,
			SortBy:            iq.sortBy,
			Descending:        iq.descending,
			Limit:             int32(limit),
			Offset:            int32(offset),
		})
		if err != nil {
			return nil, 0, err
		}
		return res.GetSeries(), int(res.GetTotal()), nil
	})
}

// listEpisodes lists the episodes matching the query.
func (s *Service) listEpisodes(ctx context.Context, iq itemQuery, offset, limit int) ([]*libraryv1.Episode, int, error) {
	return listPaged(offset, limit, func(offset, limit int) ([]*libraryv1.Episode, int, error) {
		res, err := s.library.ListEpisodes(ctx, &libraryv1.ListEpisodesRequest{
			LibraryId:         iq.libraryID,
			SeriesId:          iq.seriesID,
			SeasonId:          iq.seasonID,
			Ids:               iq.ids,
			AudioLanguages:    iq.audio,
			SubtitleLanguages: iq.subtitle,
			SortBy:            iq.episodeSortBy,
			Descending:        iq.descending,
			Limit:             int32(limit),
			Offset:            int32(offset),
		})
		if err != nil {
			return nil, 0, err
		}
		return res.GetEpisodes(), int(res.GetTotal()), nil
	})
}

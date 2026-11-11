package jellyfin

import (
	"context"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

// maxLibraryPage is the largest page the library module serves.
const maxLibraryPage = 1000

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
func (p *plugin) listMovies(ctx context.Context, iq itemQuery, offset, limit int) ([]*libraryv1.Movie, int, error) {
	return listPaged(offset, limit, func(offset, limit int) ([]*libraryv1.Movie, int, error) {
		res, err := p.library.ListMovies(ctx, &libraryv1.ListMoviesRequest{
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
func (p *plugin) listSeries(ctx context.Context, iq itemQuery, offset, limit int) ([]*libraryv1.Series, int, error) {
	return listPaged(offset, limit, func(offset, limit int) ([]*libraryv1.Series, int, error) {
		res, err := p.library.ListSeries(ctx, &libraryv1.ListSeriesRequest{
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
func (p *plugin) listEpisodes(ctx context.Context, iq itemQuery, offset, limit int) ([]*libraryv1.Episode, int, error) {
	return listPaged(offset, limit, func(offset, limit int) ([]*libraryv1.Episode, int, error) {
		res, err := p.library.ListEpisodes(ctx, &libraryv1.ListEpisodesRequest{
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

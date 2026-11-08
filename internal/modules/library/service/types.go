package service

import (
	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/store"
)

// The records are the ones of the store.
type (
	Library    = store.Library
	Movie      = store.Movie
	MediaInfo  = store.MediaInfo
	Stream     = store.Stream
	MovieQuery = store.MovieQuery

	Series       = store.Series
	Season       = store.Season
	Episode      = store.Episode
	SeriesQuery  = store.SeriesQuery
	SeasonQuery  = store.SeasonQuery
	EpisodeQuery = store.EpisodeQuery
)

// Media types of the libraries.
const (
	MediaTypeMovies  = store.MediaTypeMovies
	MediaTypeTVShows = store.MediaTypeTVShows
)

// Sort orders of ListMovies (title, added, year), ListSeries (title,
// added, year) and ListEpisodes (number, added).
const (
	SortByTitle  = store.SortByTitle
	SortByAdded  = store.SortByAdded
	SortByYear   = store.SortByYear
	SortByNumber = store.SortByNumber
)

// CreateLibraryRequest registers a library.
type CreateLibraryRequest struct {
	Name string
	// MediaType must be MediaTypeMovies or MediaTypeTVShows.
	MediaType   string
	LocationIDs []uuid.UUID
}

// MoviePage is a page of movies.
type MoviePage struct {
	Movies []Movie
	// Total is the number of movies matching the query, ignoring the
	// offset and limit.
	Total int
}

// SeriesPage is a page of series.
type SeriesPage struct {
	Series []Series
	// Total is the number of series matching the query, ignoring the
	// offset and limit.
	Total int
}

// EpisodePage is a page of episodes.
type EpisodePage struct {
	Episodes []Episode
	// Total is the number of episodes matching the query, ignoring the
	// offset and limit.
	Total int
}

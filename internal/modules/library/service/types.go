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
)

// MediaTypeMovies is the media type of the movie libraries.
const MediaTypeMovies = store.MediaTypeMovies

// Sort orders of ListMovies.
const (
	SortByTitle = store.SortByTitle
	SortByAdded = store.SortByAdded
	SortByYear  = store.SortByYear
)

// CreateLibraryRequest registers a library.
type CreateLibraryRequest struct {
	Name string
	// MediaType must be MediaTypeMovies.
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

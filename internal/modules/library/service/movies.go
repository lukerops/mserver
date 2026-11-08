package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (s *service) GetMovie(ctx context.Context, id uuid.UUID) (Movie, error) {
	return s.store.GetMovie(ctx, id)
}

func (s *service) ListMovies(ctx context.Context, q MovieQuery) (MoviePage, error) {
	switch q.SortBy {
	case "", SortByTitle, SortByAdded, SortByYear:
	default:
		return MoviePage{}, fmt.Errorf("%w: unknown sort %q", ErrInvalidArgument, q.SortBy)
	}
	if q.Offset < 0 || q.Limit < 0 {
		return MoviePage{}, fmt.Errorf("%w: negative offset or limit", ErrInvalidArgument)
	}
	movies, total, err := s.store.ListMovies(ctx, q)
	if err != nil {
		return MoviePage{}, err
	}
	return MoviePage{Movies: movies, Total: total}, nil
}

// movieNamespace derives the movie IDs from the file IDs.
var movieNamespace = uuid.MustParse("3b0c7f0e-8a4d-4c1e-9f57-6d2a1b9e4c83")

// movieID is the ID of the movie of a file: always the same for the same
// file, so that the catalog can be rebuilt from the storage events.
func movieID(file uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(movieNamespace, file[:])
}

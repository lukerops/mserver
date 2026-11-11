package handler

import (
	"context"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	"github.com/lukerops/mserver/internal/modules/library/service"
)

func (h *Handler) GetMovie(ctx context.Context, req *libraryv1.GetMovieRequest) (*libraryv1.GetMovieResponse, error) {
	libraryID, err := parseOptionalID("library_id", req.GetLibraryId())
	if err != nil {
		return nil, err
	}
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	m, err := h.svc.GetMovie(ctx, libraryID, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.GetMovieResponse{Movie: toMovie(m)}, nil
}

func (h *Handler) ListMovies(ctx context.Context, req *libraryv1.ListMoviesRequest) (*libraryv1.ListMoviesResponse, error) {
	libraryID, err := parseOptionalID("library_id", req.GetLibraryId())
	if err != nil {
		return nil, err
	}
	ids, err := parseIDs("ids", req.GetIds())
	if err != nil {
		return nil, err
	}
	limit, offset, err := page(req.GetLimit(), req.GetOffset())
	if err != nil {
		return nil, err
	}
	res, err := h.svc.ListMovies(ctx, &service.MovieQuery{
		LibraryID:         libraryID,
		IDs:               ids,
		Years:             toInts(req.GetYears()),
		AudioLanguages:    req.GetAudioLanguages(),
		SubtitleLanguages: req.GetSubtitleLanguages(),
		SortBy:            req.GetSortBy(),
		Descending:        req.GetDescending(),
		Limit:             limit,
		Offset:            offset,
	})
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &libraryv1.ListMoviesResponse{Movies: make([]*libraryv1.Movie, len(res.Movies)), Total: int32(res.Total)}
	for i := range res.Movies {
		resp.Movies[i] = toMovie(&res.Movies[i])
	}
	return resp, nil
}

package handler

import (
	"context"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	"github.com/lukerops/mserver/internal/modules/library/service"
)

func (h *Handler) GetSeries(ctx context.Context, req *libraryv1.GetSeriesRequest) (*libraryv1.GetSeriesResponse, error) {
	libraryID, err := parseOptionalID("library_id", req.GetLibraryId())
	if err != nil {
		return nil, err
	}
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	s, err := h.svc.GetSeries(ctx, libraryID, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.GetSeriesResponse{Series: toSeries(s)}, nil
}

func (h *Handler) ListSeries(ctx context.Context, req *libraryv1.ListSeriesRequest) (*libraryv1.ListSeriesResponse, error) {
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
	res, err := h.svc.ListSeries(ctx, &service.SeriesQuery{
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
	resp := &libraryv1.ListSeriesResponse{Series: make([]*libraryv1.Series, len(res.Series)), Total: int32(res.Total)}
	for i := range res.Series {
		resp.Series[i] = toSeries(&res.Series[i])
	}
	return resp, nil
}

func (h *Handler) GetSeason(ctx context.Context, req *libraryv1.GetSeasonRequest) (*libraryv1.GetSeasonResponse, error) {
	libraryID, err := parseOptionalID("library_id", req.GetLibraryId())
	if err != nil {
		return nil, err
	}
	seriesID, err := parseOptionalID("series_id", req.GetSeriesId())
	if err != nil {
		return nil, err
	}
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	s, err := h.svc.GetSeason(ctx, libraryID, seriesID, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.GetSeasonResponse{Season: toSeason(s)}, nil
}

func (h *Handler) ListSeasons(ctx context.Context, req *libraryv1.ListSeasonsRequest) (*libraryv1.ListSeasonsResponse, error) {
	libraryID, err := parseOptionalID("library_id", req.GetLibraryId())
	if err != nil {
		return nil, err
	}
	seriesID, err := parseOptionalID("series_id", req.GetSeriesId())
	if err != nil {
		return nil, err
	}
	ids, err := parseIDs("ids", req.GetIds())
	if err != nil {
		return nil, err
	}
	seasons, err := h.svc.ListSeasons(ctx, &service.SeasonQuery{LibraryID: libraryID, SeriesID: seriesID, IDs: ids})
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &libraryv1.ListSeasonsResponse{Seasons: make([]*libraryv1.Season, len(seasons))}
	for i := range seasons {
		resp.Seasons[i] = toSeason(&seasons[i])
	}
	return resp, nil
}

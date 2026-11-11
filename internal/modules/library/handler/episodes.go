package handler

import (
	"context"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	"github.com/lukerops/mserver/internal/modules/library/service"
)

func (h *Handler) GetEpisode(ctx context.Context, req *libraryv1.GetEpisodeRequest) (*libraryv1.GetEpisodeResponse, error) {
	libraryID, err := parseOptionalID("library_id", req.GetLibraryId())
	if err != nil {
		return nil, err
	}
	seriesID, err := parseOptionalID("series_id", req.GetSeriesId())
	if err != nil {
		return nil, err
	}
	seasonID, err := parseOptionalID("season_id", req.GetSeasonId())
	if err != nil {
		return nil, err
	}
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	e, err := h.svc.GetEpisode(ctx, libraryID, seriesID, seasonID, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.GetEpisodeResponse{Episode: toEpisode(e)}, nil
}

func (h *Handler) ListEpisodes(ctx context.Context, req *libraryv1.ListEpisodesRequest) (*libraryv1.ListEpisodesResponse, error) {
	libraryID, err := parseOptionalID("library_id", req.GetLibraryId())
	if err != nil {
		return nil, err
	}
	seriesID, err := parseOptionalID("series_id", req.GetSeriesId())
	if err != nil {
		return nil, err
	}
	seasonID, err := parseOptionalID("season_id", req.GetSeasonId())
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
	res, err := h.svc.ListEpisodes(ctx, &service.EpisodeQuery{
		LibraryID:         libraryID,
		SeriesID:          seriesID,
		SeasonID:          seasonID,
		IDs:               ids,
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
	resp := &libraryv1.ListEpisodesResponse{Episodes: make([]*libraryv1.Episode, len(res.Episodes)), Total: int32(res.Total)}
	for i := range res.Episodes {
		resp.Episodes[i] = toEpisode(&res.Episodes[i])
	}
	return resp, nil
}

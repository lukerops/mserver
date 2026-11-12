package handlers

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// seriesID parses the seriesId path value. When it is invalid, it answers
// 404 and returns false.
func seriesID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("seriesId"))
	if err != nil {
		notFound(w, r, "invalid seriesId")
		return uuid.Nil, false
	}
	return id, true
}

// seasons lists the seasons of a series by number, the specials (season 0)
// first. isSpecialSeason keeps only the specials (true) or only the other
// seasons (false).
func (h *Handler) seasons(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.Seasons(r.Context(), id)
	if err != nil {
		h.serviceError(w, r, "list seasons", err)
		return
	}
	q := lowerQuery(r)
	res := service.ItemQueryResult{Items: []service.Item{}}
	for _, item := range items {
		if special := q["isspecialseason"]; special != "" &&
			isTrue(special) != (item.IndexNumber != nil && *item.IndexNumber == 0) {
			continue
		}
		res.Items = append(res.Items, item)
	}
	res.TotalRecordCount = len(res.Items)
	writeJSON(w, http.StatusOK, res)
}

// episodes lists the episodes of a series, or of one of its seasons (by
// seasonId or by season number), in order. startItemId starts the list at an
// episode, before startIndex and limit apply. There are no missing episodes
// until metadata.
func (h *Handler) episodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	q := lowerQuery(r)
	start, okStart := intParam(q, "startindex")
	limit, okLimit := intParam(q, "limit")
	if !okStart || !okLimit {
		badRequest(w, r, "invalid startIndex or limit")
		return
	}
	if isTrue(q["ismissing"]) {
		writeJSON(w, http.StatusOK, service.ItemQueryResult{Items: []service.Item{}, StartIndex: start})
		return
	}

	var seasonID string
	var seasonNumber *int
	switch {
	case q["seasonid"] != "":
		season, err := uuid.Parse(q["seasonid"])
		if err != nil {
			badRequest(w, r, "invalid seasonId")
			return
		}
		seasonID = season.String()
	case q["season"] != "":
		number, err := strconv.Atoi(q["season"])
		if err != nil {
			badRequest(w, r, "invalid season")
			return
		}
		seasonNumber = &number
	}

	var startItemID string
	if s := q["startitemid"]; s != "" {
		start, err := uuid.Parse(s)
		if err != nil {
			badRequest(w, r, "invalid startItemId")
			return
		}
		startItemID = start.String()
	}

	res, err := h.svc.Episodes(r.Context(), service.EpisodeQuery{
		SeriesID:     id,
		SeasonID:     seasonID,
		SeasonNumber: seasonNumber,
		StartItemID:  startItemID,
		Start:        start,
		Limit:        limit,
		Full:         wantsField(q, "MediaSources") || wantsField(q, "MediaStreams"),
	})
	if err != nil {
		h.serviceError(w, r, "list episodes", err)
		return
	}
	signItems(res.Items, requestAuthorization(r).Token)
	writeJSON(w, http.StatusOK, res)
}

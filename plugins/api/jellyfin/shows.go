package jellyfin

import (
	"log/slog"
	"net/http"
	"strconv"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

// lookupSeries finds the series of the seriesId path value. When there is
// none, it answers 404 and returns nil.
func (p *plugin) lookupSeries(w http.ResponseWriter, r *http.Request) *libraryv1.Series {
	id, err := uuid.Parse(r.PathValue("seriesId"))
	if err != nil {
		notFound(w, r, "invalid seriesId")
		return nil
	}
	if p.library == nil {
		notFound(w, r, "series not found: no library module")
		return nil
	}
	res, err := p.library.GetSeries(r.Context(), &libraryv1.GetSeriesRequest{Id: id.String()})
	if isCode(err, connect.CodeNotFound) {
		notFound(w, r, "series not found")
		return nil
	}
	if err != nil {
		p.internalError(w, r, "get series", err)
		return nil
	}
	return res.GetSeries()
}

// listSeasons lists the seasons of a series. A series removed meanwhile
// has no seasons.
func (p *plugin) listSeasons(r *http.Request, series *libraryv1.Series) ([]*libraryv1.Season, error) {
	res, err := p.library.ListSeasons(r.Context(), &libraryv1.ListSeasonsRequest{
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

// seasons lists the seasons of a series by number, the specials (season
// 0) first. isSpecialSeason keeps only the specials (true) or only the
// other seasons (false).
func (p *plugin) seasons(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	series := p.lookupSeries(w, r)
	if series == nil {
		return
	}
	q := lowerQuery(r)
	seasons, err := p.listSeasons(r, series)
	if err != nil {
		p.internalError(w, r, "list seasons", err)
		return
	}
	res := BaseItemDtoQueryResult{Items: []any{}}
	for _, s := range seasons {
		if special := q["isspecialseason"]; special != "" && isTrue(special) != (s.GetNumber() == 0) {
			continue
		}
		res.Items = append(res.Items, p.seasonItem(s))
	}
	res.TotalRecordCount = len(res.Items)
	writeJSON(w, http.StatusOK, res)
}

// episodes lists the episodes of a series, or of one of its seasons (by
// seasonId or by season number), in order. startItemId starts the list
// at an episode, before startIndex and limit apply. There are no missing
// episodes until metadata.
func (p *plugin) episodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	series := p.lookupSeries(w, r)
	if series == nil {
		return
	}
	q := lowerQuery(r)
	start, okStart := intParam(q, "startindex")
	limit, okLimit := intParam(q, "limit")
	if !okStart || !okLimit {
		badRequest(w, r, "invalid startIndex or limit")
		return
	}
	empty := BaseItemDtoQueryResult{Items: []any{}, StartIndex: start}
	if isTrue(q["ismissing"]) {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	var seasonID string
	switch {
	case q["seasonid"] != "":
		id, err := uuid.Parse(q["seasonid"])
		if err != nil {
			badRequest(w, r, "invalid seasonId")
			return
		}
		seasonID = id.String()
	case q["season"] != "":
		number, err := strconv.Atoi(q["season"])
		if err != nil {
			badRequest(w, r, "invalid season")
			return
		}
		seasons, err := p.listSeasons(r, series)
		if err != nil {
			p.internalError(w, r, "list seasons", err)
			return
		}
		for _, s := range seasons {
			if int(s.GetNumber()) == number {
				seasonID = s.GetId()
			}
		}
		if seasonID == "" {
			writeJSON(w, http.StatusOK, empty)
			return
		}
	}
	episodes, _, err := p.listEpisodes(r.Context(), itemQuery{
		libraryID: series.GetLibraryId(),
		seriesID:  series.GetId(),
		seasonID:  seasonID,
	}, 0, 0)
	if isCode(err, connect.CodeNotFound) {
		// The season is not of the series, or was removed meanwhile.
		writeJSON(w, http.StatusOK, empty)
		return
	}
	if err != nil {
		p.internalError(w, r, "list episodes", err)
		return
	}
	if s := q["startitemid"]; s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			badRequest(w, r, "invalid startItemId")
			return
		}
		for i, e := range episodes {
			if e.GetId() == id.String() {
				episodes = episodes[i:]
				break
			}
		}
	}

	full := wantsField(q, "MediaSources") || wantsField(q, "MediaStreams")
	p.log.LogAttrs(r.Context(), slog.LevelDebug, "jellyfin: episodes query",
		slog.String("series_id", series.GetId()),
		slog.String("season_id", seasonID),
		slog.String("start_item_id", q["startitemid"]),
		slog.Int("start_index", start),
		slog.Int("limit", limit),
		slog.Int("total", len(episodes)),
	)
	res := BaseItemDtoQueryResult{Items: []any{}, TotalRecordCount: len(episodes), StartIndex: start}
	for _, e := range paginate(episodes, start, limit) {
		res.Items = append(res.Items, p.episodeItem(e, full))
	}
	signItems(res.Items, requestAuthorization(r).Token)
	writeJSON(w, http.StatusOK, res)
}

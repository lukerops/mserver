package jellyfin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	library "github.com/lukerops/mserver/internal/modules/library/client"
	libraryrouter "github.com/lukerops/mserver/internal/modules/library/router"
)

// lookupSeries finds the series of the seriesId path value. When there is
// none, it answers 404 and returns false.
func (p *plugin) lookupSeries(w http.ResponseWriter, r *http.Request) (libraryrouter.SeriesV1, bool) {
	id, err := uuid.Parse(r.PathValue("seriesId"))
	if err != nil || p.library == nil {
		http.NotFound(w, r)
		return libraryrouter.SeriesV1{}, false
	}
	s, err := p.library.GetSeries(r.Context(), uuid.Nil, id)
	if errors.Is(err, library.ErrNotFound) {
		http.NotFound(w, r)
		return libraryrouter.SeriesV1{}, false
	}
	if err != nil {
		p.internalError(w, r, "get series", err)
		return libraryrouter.SeriesV1{}, false
	}
	return s, true
}

// seasons lists the seasons of a series by number, the specials (season
// 0) first. isSpecialSeason keeps only the specials (true) or only the
// other seasons (false).
func (p *plugin) seasons(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	series, ok := p.lookupSeries(w, r)
	if !ok {
		return
	}
	q := lowerQuery(r)
	seasons, err := p.library.ListSeasons(r.Context(), series.LibraryID, series.ID, libraryrouter.SeasonQueryV1{})
	if errors.Is(err, library.ErrNotFound) {
		seasons = nil // the series was removed meanwhile
	} else if err != nil {
		p.internalError(w, r, "list seasons", err)
		return
	}
	res := BaseItemDtoQueryResult{Items: []any{}}
	for _, s := range seasons {
		if special := q["isspecialseason"]; special != "" && isTrue(special) != (s.Number == 0) {
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
	series, ok := p.lookupSeries(w, r)
	if !ok {
		return
	}
	q := lowerQuery(r)
	start, okStart := intParam(q, "startindex")
	limit, okLimit := intParam(q, "limit")
	if !okStart || !okLimit {
		http.Error(w, "invalid startIndex or limit", http.StatusBadRequest)
		return
	}
	empty := BaseItemDtoQueryResult{Items: []any{}, StartIndex: start}
	if isTrue(q["ismissing"]) {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	var seasonID uuid.UUID
	switch {
	case q["seasonid"] != "":
		id, err := uuid.Parse(q["seasonid"])
		if err != nil {
			http.Error(w, "invalid seasonId", http.StatusBadRequest)
			return
		}
		seasonID = id
	case q["season"] != "":
		number, err := strconv.Atoi(q["season"])
		if err != nil {
			http.Error(w, "invalid season", http.StatusBadRequest)
			return
		}
		seasons, err := p.library.ListSeasons(r.Context(), series.LibraryID, series.ID, libraryrouter.SeasonQueryV1{})
		if err != nil && !errors.Is(err, library.ErrNotFound) {
			p.internalError(w, r, "list seasons", err)
			return
		}
		for _, s := range seasons {
			if s.Number == number {
				seasonID = s.ID
			}
		}
		if seasonID == uuid.Nil {
			writeJSON(w, http.StatusOK, empty)
			return
		}
	}
	page, err := p.library.ListEpisodes(r.Context(), series.LibraryID, series.ID, seasonID, libraryrouter.EpisodeQueryV1{})
	if errors.Is(err, library.ErrNotFound) {
		// The season is not of the series, or was removed meanwhile.
		writeJSON(w, http.StatusOK, empty)
		return
	}
	if err != nil {
		p.internalError(w, r, "list episodes", err)
		return
	}
	episodes := page.Episodes
	if s := q["startitemid"]; s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			http.Error(w, "invalid startItemId", http.StatusBadRequest)
			return
		}
		for i, e := range episodes {
			if e.ID == id {
				episodes = episodes[i:]
				break
			}
		}
	}

	full := wantsField(q, "MediaSources") || wantsField(q, "MediaStreams")
	res := BaseItemDtoQueryResult{Items: []any{}, TotalRecordCount: len(episodes), StartIndex: start}
	for _, e := range paginate(episodes, start, limit) {
		res.Items = append(res.Items, p.episodeItem(e, full))
	}
	writeJSON(w, http.StatusOK, res)
}

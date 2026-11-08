package router

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/service"
)

func (rt *router) listSeries(w http.ResponseWriter, r *http.Request) {
	q, err := ParseSeriesQueryV1(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorV1{Error: err.Error()})
		return
	}
	page, err := rt.svc.ListSeries(r.Context(), service.SeriesQuery{
		LibraryID:         q.LibraryID,
		IDs:               q.IDs,
		Years:             q.Years,
		AudioLanguages:    q.AudioLanguages,
		SubtitleLanguages: q.SubtitleLanguages,
		SortBy:            q.SortBy,
		Descending:        q.Descending,
		Offset:            q.Offset,
		Limit:             q.Limit,
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := SeriesListV1{Series: make([]SeriesV1, len(page.Series)), Total: page.Total}
	for i, s := range page.Series {
		resp.Series[i] = toSeriesV1(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "series_id", false)
	if !ok {
		return
	}
	s, err := rt.svc.GetSeries(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSeriesV1(s))
}

func (rt *router) listSeasons(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := pathUUID(w, r, "series_id", true)
	if !ok {
		return
	}
	q, err := ParseSeasonQueryV1(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorV1{Error: err.Error()})
		return
	}
	seasons, err := rt.svc.ListSeasons(r.Context(), service.SeasonQuery{
		LibraryID: q.LibraryID,
		SeriesID:  seriesID,
		IDs:       q.IDs,
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := SeasonListV1{Seasons: make([]SeasonV1, len(seasons))}
	for i, s := range seasons {
		resp.Seasons[i] = toSeasonV1(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getSeason(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := pathUUID(w, r, "series_id", true)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "season_id", false)
	if !ok {
		return
	}
	s, err := rt.svc.GetSeason(r.Context(), seriesID, id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSeasonV1(s))
}

func (rt *router) listEpisodes(w http.ResponseWriter, r *http.Request) {
	seriesID, seasonID, ok := pathParents(w, r)
	if !ok {
		return
	}
	q, err := ParseEpisodeQueryV1(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorV1{Error: err.Error()})
		return
	}
	page, err := rt.svc.ListEpisodes(r.Context(), service.EpisodeQuery{
		LibraryID:         q.LibraryID,
		SeriesID:          seriesID,
		SeasonID:          seasonID,
		IDs:               q.IDs,
		AudioLanguages:    q.AudioLanguages,
		SubtitleLanguages: q.SubtitleLanguages,
		SortBy:            q.SortBy,
		Descending:        q.Descending,
		Offset:            q.Offset,
		Limit:             q.Limit,
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := EpisodeListV1{Episodes: make([]EpisodeV1, len(page.Episodes)), Total: page.Total}
	for i, e := range page.Episodes {
		resp.Episodes[i] = toEpisodeV1(e)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getEpisode(w http.ResponseWriter, r *http.Request) {
	seriesID, seasonID, ok := pathParents(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "episode_id", false)
	if !ok {
		return
	}
	e, err := rt.svc.GetEpisode(r.Context(), seriesID, seasonID, id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toEpisodeV1(e))
}

// pathParents parses the {series_id} and {season_id} of the path, which
// may be the Wildcard. When it fails, the error response is already
// written.
func pathParents(w http.ResponseWriter, r *http.Request) (seriesID, seasonID uuid.UUID, ok bool) {
	if seriesID, ok = pathUUID(w, r, "series_id", true); !ok {
		return
	}
	seasonID, ok = pathUUID(w, r, "season_id", true)
	return
}

// Values encodes q as query parameters.
func (q SeriesQueryV1) Values() url.Values {
	// The parameters are the ones of the movies.
	return MovieQueryV1(q).Values()
}

// ParseSeriesQueryV1 decodes the query parameters of GET /v1/series.
func ParseSeriesQueryV1(v url.Values) (SeriesQueryV1, error) {
	q, err := ParseMovieQueryV1(v)
	return SeriesQueryV1(q), err
}

// Values encodes q as query parameters.
func (q SeasonQueryV1) Values() url.Values {
	v := url.Values{}
	setID(v, "library_id", q.LibraryID)
	setIDs(v, "ids", q.IDs)
	return v
}

// ParseSeasonQueryV1 decodes the query parameters of
// GET /v1/series/{series_id}/seasons.
func ParseSeasonQueryV1(v url.Values) (SeasonQueryV1, error) {
	var (
		q   SeasonQueryV1
		err error
	)
	if q.LibraryID, err = parseID(v, "library_id"); err != nil {
		return q, err
	}
	q.IDs, err = parseIDs(v, "ids")
	return q, err
}

// Values encodes q as query parameters.
func (q EpisodeQueryV1) Values() url.Values {
	return MovieQueryV1{
		LibraryID:         q.LibraryID,
		IDs:               q.IDs,
		AudioLanguages:    q.AudioLanguages,
		SubtitleLanguages: q.SubtitleLanguages,
		SortBy:            q.SortBy,
		Descending:        q.Descending,
		Offset:            q.Offset,
		Limit:             q.Limit,
	}.Values()
}

// ParseEpisodeQueryV1 decodes the query parameters of
// GET /v1/series/{series_id}/seasons/{season_id}/episodes.
func ParseEpisodeQueryV1(v url.Values) (EpisodeQueryV1, error) {
	if s := v.Get("years"); s != "" {
		return EpisodeQueryV1{}, &queryError{"years", s}
	}
	m, err := ParseMovieQueryV1(v)
	return EpisodeQueryV1{
		LibraryID:         m.LibraryID,
		IDs:               m.IDs,
		AudioLanguages:    m.AudioLanguages,
		SubtitleLanguages: m.SubtitleLanguages,
		SortBy:            m.SortBy,
		Descending:        m.Descending,
		Offset:            m.Offset,
		Limit:             m.Limit,
	}, err
}

func setID(v url.Values, name string, id uuid.UUID) {
	if id != uuid.Nil {
		v.Set(name, id.String())
	}
}

func setIDs(v url.Values, name string, ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	v.Set(name, strings.Join(s, ","))
}

// parseID parses an optional id parameter.
func parseID(v url.Values, name string) (uuid.UUID, error) {
	s := v.Get(name)
	if s == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, &queryError{name, s}
	}
	return id, nil
}

// parseIDs parses an optional comma-separated list of ids.
func parseIDs(v url.Values, name string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if s := v.Get(name); s != "" {
		for part := range strings.SplitSeq(s, ",") {
			id, err := uuid.Parse(strings.TrimSpace(part))
			if err != nil {
				return nil, &queryError{name, part}
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

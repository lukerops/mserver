package router

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/service"
)

func (rt *router) listMovies(w http.ResponseWriter, r *http.Request) {
	parents, ok := pathParents(w, r, "library_id")
	if !ok {
		return
	}
	q, err := ParseMovieQueryV1(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorV1{Error: err.Error()})
		return
	}
	page, err := rt.svc.ListMovies(r.Context(), service.MovieQuery{
		LibraryID:         parents[0],
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
	resp := MovieListV1{Movies: make([]MovieV1, len(page.Movies)), Total: page.Total}
	for i, m := range page.Movies {
		resp.Movies[i] = toMovieV1(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getMovie(w http.ResponseWriter, r *http.Request) {
	parents, ok := pathParents(w, r, "library_id")
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "movie_id", false)
	if !ok {
		return
	}
	m, err := rt.svc.GetMovie(r.Context(), parents[0], id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMovieV1(m))
}

// Values encodes q as query parameters.
func (q MovieQueryV1) Values() url.Values {
	v := url.Values{}
	if len(q.IDs) > 0 {
		ids := make([]string, len(q.IDs))
		for i, id := range q.IDs {
			ids[i] = id.String()
		}
		v.Set("ids", strings.Join(ids, ","))
	}
	if len(q.Years) > 0 {
		years := make([]string, len(q.Years))
		for i, y := range q.Years {
			years[i] = strconv.Itoa(y)
		}
		v.Set("years", strings.Join(years, ","))
	}
	if len(q.AudioLanguages) > 0 {
		v.Set("audio_languages", strings.Join(q.AudioLanguages, ","))
	}
	if len(q.SubtitleLanguages) > 0 {
		v.Set("subtitle_languages", strings.Join(q.SubtitleLanguages, ","))
	}
	if q.SortBy != "" {
		v.Set("sort_by", q.SortBy)
	}
	if q.Descending {
		v.Set("order", "desc")
	}
	if q.Offset != 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	if q.Limit != 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	return v
}

// ParseMovieQueryV1 decodes the query parameters of
// GET /v1/libraries/{library_id}/movies.
func ParseMovieQueryV1(v url.Values) (MovieQueryV1, error) {
	var q MovieQueryV1
	if s := v.Get("ids"); s != "" {
		for part := range strings.SplitSeq(s, ",") {
			id, err := uuid.Parse(strings.TrimSpace(part))
			if err != nil {
				return q, &queryError{"ids", part}
			}
			q.IDs = append(q.IDs, id)
		}
	}
	if s := v.Get("years"); s != "" {
		for part := range strings.SplitSeq(s, ",") {
			y, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || y <= 0 {
				return q, &queryError{"years", part}
			}
			q.Years = append(q.Years, y)
		}
	}
	q.AudioLanguages = splitList(v.Get("audio_languages"))
	q.SubtitleLanguages = splitList(v.Get("subtitle_languages"))
	q.SortBy = v.Get("sort_by")
	switch s := v.Get("order"); s {
	case "", "asc":
	case "desc":
		q.Descending = true
	default:
		return q, &queryError{"order", s}
	}
	for name, dst := range map[string]*int{"offset": &q.Offset, "limit": &q.Limit} {
		if s := v.Get(name); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				return q, &queryError{name, s}
			}
			*dst = n
		}
	}
	return q, nil
}

type queryError struct{ param, value string }

// splitList splits a comma-separated parameter, dropping empty values.
func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (e *queryError) Error() string {
	return "invalid " + e.param + " " + strconv.Quote(e.value)
}

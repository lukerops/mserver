package jellyfin

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	library "github.com/lukerops/mserver/internal/modules/library/client"
	libraryrouter "github.com/lukerops/mserver/internal/modules/library/router"
)

// defaultLatestLimit is the number of items of /Items/Latest when the
// client sends no limit, as in Jellyfin.
const defaultLatestLimit = 20

// userViews lists the libraries ("My Media").
func (p *plugin) userViews(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	p.writeViews(w, r)
}

func (p *plugin) writeViews(w http.ResponseWriter, r *http.Request) {
	res := BaseItemDtoQueryResult{Items: []any{}}
	if p.library != nil {
		libraries, err := p.library.ListLibraries(r.Context())
		if err != nil {
			p.internalError(w, r, "list libraries", err)
			return
		}
		for _, l := range libraries {
			res.Items = append(res.Items, p.libraryItem(l))
		}
	}
	res.TotalRecordCount = len(res.Items)
	writeJSON(w, http.StatusOK, res)
}

// items lists the movies of a library, or of every library when the
// query is recursive. Filters that depend on the user state (favorites,
// played...) or on metadata the library does not have yet (genres,
// people...) match nothing.
func (p *plugin) items(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
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
	if p.library == nil || unsupportedFilter(q) {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	mq := libraryrouter.MovieQueryV1{Offset: start, Limit: limit}
	mq.SortBy, mq.Descending = movieSort(q["sortby"], q["sortorder"])
	if s := q["ids"]; s != "" {
		for part := range strings.SplitSeq(s, ",") {
			id, err := uuid.Parse(strings.TrimSpace(part))
			if err != nil {
				http.Error(w, "invalid ids", http.StatusBadRequest)
				return
			}
			mq.IDs = append(mq.IDs, id)
		}
	}
	for _, s := range list(q["years"]) {
		y, err := strconv.Atoi(s)
		if err != nil || y <= 0 {
			http.Error(w, "invalid years", http.StatusBadRequest)
			return
		}
		mq.Years = append(mq.Years, y)
	}
	mq.AudioLanguages = list(q["audiolanguages"])
	mq.SubtitleLanguages = list(q["subtitlelanguages"])
	switch parent := q["parentid"]; {
	case parent != "":
		id, err := uuid.Parse(parent)
		if err != nil {
			http.Error(w, "invalid parentId", http.StatusBadRequest)
			return
		}
		_, err = p.library.GetLibrary(r.Context(), id)
		if errors.Is(err, library.ErrNotFound) {
			writeJSON(w, http.StatusOK, empty) // e.g. a movie: it has no children
			return
		}
		if err != nil {
			p.internalError(w, r, "get library", err)
			return
		}
		mq.LibraryID = id
	case mq.IDs == nil && !isTrue(q["recursive"]):
		// The children of the root of the user are its views.
		p.writeViews(w, r)
		return
	}
	if !includesType(q, "Movie") {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	page, err := p.library.ListMovies(r.Context(), mq)
	if err != nil {
		p.internalError(w, r, "list movies", err)
		return
	}
	full := wantsField(q, "MediaSources") || wantsField(q, "MediaStreams")
	res := BaseItemDtoQueryResult{Items: make([]any, len(page.Movies)), TotalRecordCount: page.Total, StartIndex: start}
	for i, m := range page.Movies {
		res.Items[i] = p.movieItem(m, full)
	}
	writeJSON(w, http.StatusOK, res)
}

// latestItems lists the movies added last, of a library or of every
// library. The response is an array, not a query result.
func (p *plugin) latestItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	q := lowerQuery(r)
	limit, ok := intParam(q, "limit")
	if !ok {
		http.Error(w, "invalid limit", http.StatusBadRequest)
		return
	}
	if limit == 0 {
		limit = defaultLatestLimit
	}
	items := []BaseItemDto{}
	if p.library == nil || isTrue(q["isplayed"]) || !includesType(q, "Movie") {
		writeJSON(w, http.StatusOK, items)
		return
	}

	mq := libraryrouter.MovieQueryV1{SortBy: "added", Descending: true, Limit: limit}
	if parent := q["parentid"]; parent != "" {
		id, err := uuid.Parse(parent)
		if err != nil {
			http.Error(w, "invalid parentId", http.StatusBadRequest)
			return
		}
		_, err = p.library.GetLibrary(r.Context(), id)
		if errors.Is(err, library.ErrNotFound) {
			writeJSON(w, http.StatusOK, items)
			return
		}
		if err != nil {
			p.internalError(w, r, "get library", err)
			return
		}
		mq.LibraryID = id
	}
	page, err := p.library.ListMovies(r.Context(), mq)
	if err != nil {
		p.internalError(w, r, "list movies", err)
		return
	}
	full := wantsField(q, "MediaSources") || wantsField(q, "MediaStreams")
	for _, m := range page.Movies {
		items = append(items, p.movieItem(m, full))
	}
	writeJSON(w, http.StatusOK, items)
}

// item returns a movie, with its media sources, or a library.
func (p *plugin) item(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	movie, lib, ok := p.lookupItem(w, r)
	if !ok {
		return
	}
	if movie != nil {
		writeJSON(w, http.StatusOK, p.movieItem(*movie, true))
		return
	}
	writeJSON(w, http.StatusOK, p.libraryItem(*lib))
}

// ancestors returns the parents of an item: the library of a movie. A
// library has no parents visible to the user.
func (p *plugin) ancestors(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	movie, _, ok := p.lookupItem(w, r)
	if !ok {
		return
	}
	parents := []BaseItemDto{}
	if movie != nil {
		l, err := p.library.GetLibrary(r.Context(), movie.LibraryID)
		switch {
		case err == nil:
			parents = append(parents, p.libraryItem(l))
		case !errors.Is(err, library.ErrNotFound): // removed meanwhile: no parents
			p.internalError(w, r, "get library", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, parents)
}

// themeMedia answers that the item has no theme songs or videos.
func (p *plugin) themeMedia(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	if _, _, ok := p.lookupItem(w, r); !ok {
		return
	}
	owner := jellyfinID(r.PathValue("itemId"))
	empty := ThemeMediaResult{Items: []BaseItemDto{}, OwnerId: owner}
	writeJSON(w, http.StatusOK, AllThemeMediaResult{
		ThemeVideosResult:     empty,
		ThemeSongsResult:      empty,
		SoundtrackSongsResult: empty,
	})
}

// collections answers that the item belongs to no collection (box set).
func (p *plugin) collections(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	if _, _, ok := p.lookupItem(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []any{}})
}

// queryFiltersLegacy lists the years of the movies of the query, so that
// the library can be filtered by them. The movies have no genres, tags or
// ratings until metadata.
func (p *plugin) queryFiltersLegacy(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	movies, ok := p.filterMovies(w, r)
	if !ok {
		return
	}
	years := []int{}
	for _, m := range movies {
		if m.Year > 0 && !slices.Contains(years, m.Year) {
			years = append(years, m.Year)
		}
	}
	slices.Sort(years)
	writeJSON(w, http.StatusOK, QueryFiltersLegacy{
		Genres:          []string{},
		Tags:            []string{},
		OfficialRatings: []string{},
		Years:           years,
	})
}

// queryFilters lists the audio and subtitle languages of the movies of
// the query. The movies have no genres or tags until metadata.
func (p *plugin) queryFilters(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	movies, ok := p.filterMovies(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, QueryFilters{
		Genres:            []NameGuidPair{},
		Tags:              []string{},
		AudioLanguages:    streamLanguages(movies, "audio"),
		SubtitleLanguages: streamLanguages(movies, "subtitle"),
	})
}

// filterMovies returns the movies the filters of /Items/Filters and
// /Items/Filters2 are computed from: the ones of the parentId library,
// or of every library.
func (p *plugin) filterMovies(w http.ResponseWriter, r *http.Request) ([]libraryrouter.MovieV1, bool) {
	q := lowerQuery(r)
	if p.library == nil || !includesType(q, "Movie") {
		return nil, true
	}
	if mediaTypes := q["mediatypes"]; mediaTypes != "" && !contains(mediaTypes, "Video") {
		return nil, true
	}
	var mq libraryrouter.MovieQueryV1
	if parent := q["parentid"]; parent != "" {
		id, err := uuid.Parse(parent)
		if err != nil {
			http.Error(w, "invalid parentId", http.StatusBadRequest)
			return nil, false
		}
		_, err = p.library.GetLibrary(r.Context(), id)
		if errors.Is(err, library.ErrNotFound) {
			return nil, true // e.g. a movie: it has no children
		}
		if err != nil {
			p.internalError(w, r, "get library", err)
			return nil, false
		}
		mq.LibraryID = id
	}
	page, err := p.library.ListMovies(r.Context(), mq)
	if err != nil {
		p.internalError(w, r, "list movies", err)
		return nil, false
	}
	return page.Movies, true
}

// streamLanguages lists the known languages of the streams of type typ,
// by name. The value is the language code, which /Items filters on.
func streamLanguages(movies []libraryrouter.MovieV1, typ string) []NameValuePair {
	languages := []NameValuePair{}
	seen := map[string]bool{}
	for _, m := range movies {
		if m.MediaInfo == nil {
			continue
		}
		for _, s := range m.MediaInfo.Streams {
			code := strings.ToLower(s.Language)
			if s.Type != typ || code == "" || code == "und" || seen[code] {
				continue
			}
			seen[code] = true
			languages = append(languages, NameValuePair{Name: languageName(code), Value: code})
		}
	}
	slices.SortFunc(languages, func(a, b NameValuePair) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Value, b.Value))
	})
	return languages
}

// lookupItem finds the movie or the library of the itemId path value.
// When there is none, it answers 404 and returns false.
func (p *plugin) lookupItem(w http.ResponseWriter, r *http.Request) (*libraryrouter.MovieV1, *libraryrouter.LibraryV1, bool) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil || p.library == nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	m, err := p.library.GetMovie(r.Context(), id)
	if err == nil {
		return &m, nil, true
	}
	if !errors.Is(err, library.ErrNotFound) {
		p.internalError(w, r, "get movie", err)
		return nil, nil, false
	}
	l, err := p.library.GetLibrary(r.Context(), id)
	if errors.Is(err, library.ErrNotFound) {
		http.NotFound(w, r)
		return nil, nil, false
	}
	if err != nil {
		p.internalError(w, r, "get library", err)
		return nil, nil, false
	}
	return nil, &l, true
}

// emptyItems answers the item queries that have no results yet (intros,
// similar items).
func (p *plugin) emptyItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []any{}})
}

// emptyItemList answers the item lists that are always empty yet
// (special features, local trailers).
func (p *plugin) emptyItemList(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, []BaseItemDto{})
}

func (p *plugin) internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	p.log.ErrorContext(r.Context(), "jellyfin: "+op, "error", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

// lowerQuery returns the query parameters keyed by the lower-case name:
// Jellyfin parameter names are case insensitive (jellyfin-web sends
// "ParentId", the OpenAPI says "parentId"). Repeated parameters
// ("fields=A&fields=B") are joined with commas, like the comma-delimited
// form ("fields=A,B").
func lowerQuery(r *http.Request) map[string]string {
	q := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) == 0 {
			continue
		}
		k = strings.ToLower(k)
		if q[k] != "" {
			q[k] += ","
		}
		q[k] += strings.Join(v, ",")
	}
	return q
}

// intParam parses a non-negative integer parameter; a missing one is zero.
func intParam(q map[string]string, name string) (int, bool) {
	s := q[name]
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 0
}

func isTrue(s string) bool { return strings.EqualFold(s, "true") }

// list splits a comma-separated parameter into lower-case values.
func list(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func contains(s, value string) bool {
	for _, v := range list(s) {
		if v == strings.ToLower(value) {
			return true
		}
	}
	return false
}

// includesType reports whether the type filters of the query accept
// items of type typ.
func includesType(q map[string]string, typ string) bool {
	if include := q["includeitemtypes"]; include != "" && !contains(include, typ) {
		return false
	}
	return !contains(q["excludeitemtypes"], typ)
}

func wantsField(q map[string]string, field string) bool {
	return contains(q["fields"], field)
}

// unsupportedFilter reports whether the query filters on something no
// movie has yet: user state (until watchstate) or metadata (until
// metadata).
func unsupportedFilter(q map[string]string) bool {
	for _, name := range []string{"isfavorite", "isplayed", "isresumable", "islocked", "isplaceholder"} {
		if isTrue(q[name]) {
			return true
		}
	}
	for _, f := range list(q["filters"]) {
		switch f {
		case "isfavorite", "isplayed", "isresumable", "likes", "dislikes", "isfavoriteorlikes", "isfolder":
			return true
		}
	}
	for _, name := range []string{
		"searchterm", "genres", "genreids", "personids", "person", "persontypes", "studios",
		"studioids", "tags", "officialratings", "artistids", "albumids", "namestartswith",
		"namestartswithorgreater", "namelessthan", "minpremieredate", "maxpremieredate",
	} {
		if q[name] != "" {
			return true
		}
	}
	if mediaTypes := q["mediatypes"]; mediaTypes != "" && !contains(mediaTypes, "Video") {
		return true
	}
	return false
}

// movieSort maps the Jellyfin sort (the first of sortBy and sortOrder) to
// the library sort.
func movieSort(sortBy, sortOrder string) (string, bool) {
	by := "title"
	if fields := list(sortBy); len(fields) > 0 {
		switch fields[0] {
		case "datecreated":
			by = "added"
		case "productionyear", "premieredate":
			by = "year"
		}
	}
	orders := list(sortOrder)
	return by, len(orders) > 0 && orders[0] == "descending"
}

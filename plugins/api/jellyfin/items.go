package jellyfin

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

// defaultLatestLimit is the number of items of /Items/Latest when the
// client sends no limit, as in Jellyfin.
const defaultLatestLimit = 20

// latestEpisodesWindow is how many recent episodes /Items/Latest groups
// by series, per item asked.
const latestEpisodesWindow = 5

// Media types of the libraries, which are also their Jellyfin collection
// types.
const (
	mediaTypeMovies  = "movies"
	mediaTypeTVShows = "tvshows"
)

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
		libraries, err := p.library.ListLibraries(r.Context(), &libraryv1.ListLibrariesRequest{})
		if err != nil {
			p.internalError(w, r, "list libraries", err)
			return
		}
		for _, l := range libraries.GetLibraries() {
			res.Items = append(res.Items, p.libraryItem(l))
		}
	}
	res.TotalRecordCount = len(res.Items)
	writeJSON(w, http.StatusOK, res)
}

// itemRef is an item of the catalog: exactly one field is set, unless the
// item does not exist.
type itemRef struct {
	movie   *libraryv1.Movie
	episode *libraryv1.Episode
	series  *libraryv1.Series
	season  *libraryv1.Season
	library *libraryv1.Library
}

func (ref itemRef) exists() bool {
	return ref.movie != nil || ref.episode != nil || ref.series != nil || ref.season != nil || ref.library != nil
}

// isFolder reports whether the item has children /Items can list.
func (ref itemRef) isFolder() bool {
	return ref.library != nil || ref.series != nil || ref.season != nil
}

// findItem finds the item with an id, which may be of any type. A missing
// item is not an error: the returned itemRef does not exist.
func (p *plugin) findItem(ctx context.Context, id uuid.UUID) (itemRef, error) {
	key := id.String()
	if res, err := p.library.GetMovie(ctx, &libraryv1.GetMovieRequest{Id: key}); err == nil {
		return itemRef{movie: res.GetMovie()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := p.library.GetEpisode(ctx, &libraryv1.GetEpisodeRequest{Id: key}); err == nil {
		return itemRef{episode: res.GetEpisode()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := p.library.GetSeries(ctx, &libraryv1.GetSeriesRequest{Id: key}); err == nil {
		return itemRef{series: res.GetSeries()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := p.library.GetSeason(ctx, &libraryv1.GetSeasonRequest{Id: key}); err == nil {
		return itemRef{season: res.GetSeason()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	if res, err := p.library.GetLibrary(ctx, &libraryv1.GetLibraryRequest{Id: key}); err == nil {
		return itemRef{library: res.GetLibrary()}, nil
	} else if !isCode(err, connect.CodeNotFound) {
		return itemRef{}, err
	}
	return itemRef{}, nil
}

// lookupItem finds the item of the itemId path value. When there is none,
// it answers 404 and returns false.
func (p *plugin) lookupItem(w http.ResponseWriter, r *http.Request) (itemRef, bool) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil {
		notFound(w, r, "invalid itemId")
		return itemRef{}, false
	}
	if p.library == nil {
		notFound(w, r, "item not found: no library module")
		return itemRef{}, false
	}
	ref, err := p.findItem(r.Context(), id)
	if err != nil {
		p.internalError(w, r, "find item", err)
		return itemRef{}, false
	}
	if !ref.exists() {
		notFound(w, r, "item not found")
		return itemRef{}, false
	}
	return ref, true
}

// parentScope resolves the parentId parameter. It returns false if the
// parent is not a folder (e.g. a movie, or a missing item): it has no
// children. On errors, the response is already written and the status is
// set.
func (p *plugin) parentScope(w http.ResponseWriter, r *http.Request, parent string) (itemRef, bool, bool) {
	id, err := uuid.Parse(parent)
	if err != nil {
		badRequest(w, r, "invalid parentId")
		return itemRef{}, false, false
	}
	ref, err := p.findItem(r.Context(), id)
	if err != nil {
		p.internalError(w, r, "find item", err)
		return itemRef{}, false, false
	}
	return ref, ref.isFolder(), true
}

// itemQuery holds the filters and the sort of /Items. The IDs are UUIDs in
// the canonical form, as the library takes them.
type itemQuery struct {
	ids                     []string
	years                   []int32
	audio, subtitle         []string
	sortBy, episodeSortBy   string
	descending              bool
	full                    bool
	libraryID               string
	seriesID, seasonID      string
	hasLanguageOrYearFilter bool
}

// source lists a page of the items of one type, and the number of items
// of that type matching the query.
type source func(ctx context.Context, offset, limit int) ([]any, int, error)

// items lists the children of a folder (a library, a series or a season),
// or its descendants when the query is recursive, or the items of every
// library. Filters that depend on the user state (favorites, played...) or
// on metadata the library does not have yet (genres, people...) match
// nothing.
func (p *plugin) items(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
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
	if p.library == nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	if f := unsupportedFilter(q); f != "" {
		p.log.DebugContext(r.Context(), "jellyfin: items: unsupported filter, empty result", slog.String("filter", f))
		writeJSON(w, http.StatusOK, empty)
		return
	}

	var iq itemQuery
	if s := q["ids"]; s != "" {
		for part := range strings.SplitSeq(s, ",") {
			id, err := uuid.Parse(strings.TrimSpace(part))
			if err != nil {
				badRequest(w, r, "invalid ids")
				return
			}
			iq.ids = append(iq.ids, id.String())
		}
	}
	for _, s := range list(q["years"]) {
		y, err := strconv.Atoi(s)
		if err != nil || y <= 0 {
			badRequest(w, r, "invalid years")
			return
		}
		iq.years = append(iq.years, int32(y))
	}
	iq.audio = list(q["audiolanguages"])
	iq.subtitle = list(q["subtitlelanguages"])
	iq.hasLanguageOrYearFilter = iq.years != nil || iq.audio != nil || iq.subtitle != nil
	iq.sortBy, iq.descending = movieSort(q["sortby"], q["sortorder"])
	iq.episodeSortBy = episodeSort(q["sortby"])
	iq.full = wantsField(q, "MediaSources") || wantsField(q, "MediaStreams")

	recursive := isTrue(q["recursive"])
	var kinds []string
	switch parent := q["parentid"]; {
	case parent != "":
		scope, folder, ok := p.parentScope(w, r, parent)
		if !ok {
			return
		}
		if !folder {
			writeJSON(w, http.StatusOK, empty) // e.g. a movie: it has no children
			return
		}
		switch {
		case scope.season != nil:
			iq.seasonID = scope.season.GetId()
			kinds = []string{"Episode"}
		case scope.series != nil:
			iq.seriesID = scope.series.GetId()
			kinds = []string{"Season", "Episode"}
		case scope.library.GetMediaType() == mediaTypeTVShows:
			iq.libraryID = scope.library.GetId()
			kinds = []string{"Series", "Season", "Episode"}
		default:
			iq.libraryID = scope.library.GetId()
			kinds = []string{"Movie"}
		}
	case iq.ids == nil && !recursive:
		// The children of the root of the user are its views.
		p.writeViews(w, r)
		return
	default:
		kinds = []string{"Movie", "Series", "Season", "Episode"}
	}
	// Without types to include, a query lists the children of the folder,
	// unless it is recursive or asks for items by id.
	if q["includeitemtypes"] == "" && !recursive && iq.ids == nil {
		kinds = kinds[:1]
	}

	var sources []source
	var sourceKinds []string
	for _, kind := range kinds {
		if !includesType(q, kind) {
			continue
		}
		n := len(sources)
		switch kind {
		case "Movie":
			sources = append(sources, p.movieSource(iq))
		case "Series":
			sources = append(sources, p.seriesSource(iq))
		case "Season":
			// Seasons have no years or streams of their own.
			if !iq.hasLanguageOrYearFilter {
				sources = append(sources, p.seasonSource(iq))
			}
		case "Episode":
			// Episodes have no year until metadata.
			if iq.years == nil {
				sources = append(sources, p.episodeSource(iq))
			}
		}
		if len(sources) > n {
			sourceKinds = append(sourceKinds, kind)
		}
	}
	if ctx := r.Context(); p.log.Enabled(ctx, slog.LevelDebug) {
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: items query",
			slog.String("parent_id", q["parentid"]),
			slog.Bool("recursive", recursive),
			slog.String("types", strings.Join(sourceKinds, ",")),
			slog.Int("ids", len(iq.ids)),
			slog.String("years", fmt.Sprint(iq.years)),
			slog.String("audio_languages", strings.Join(iq.audio, ",")),
			slog.String("subtitle_languages", strings.Join(iq.subtitle, ",")),
			slog.String("sort_by", iq.sortBy),
			slog.Bool("descending", iq.descending),
			slog.Int("start_index", start),
			slog.Int("limit", limit),
			slog.String("ignored_params", strings.Join(ignoredParams(q), ",")),
		)
	}
	res, err := collect(r.Context(), sources, start, limit)
	if err != nil {
		p.internalError(w, r, "list items", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// collect concatenates the sources, returning the page from start of at
// most limit items (zero means no limit).
func collect(ctx context.Context, sources []source, start, limit int) (BaseItemDtoQueryResult, error) {
	res := BaseItemDtoQueryResult{Items: []any{}, StartIndex: start}
	offset := start
	for _, src := range sources {
		n := 0
		if limit > 0 {
			n = limit - len(res.Items)
			if n <= 0 {
				// The page is full: only the total of the source is needed.
				_, total, err := src(ctx, 0, 1)
				if err != nil {
					return res, err
				}
				res.TotalRecordCount += total
				continue
			}
		}
		items, total, err := src(ctx, offset, n)
		if err != nil {
			return res, err
		}
		res.Items = append(res.Items, items...)
		res.TotalRecordCount += total
		offset = max(0, offset-total)
	}
	return res, nil
}

func (p *plugin) movieSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]any, int, error) {
		movies, total, err := p.listMovies(ctx, iq, offset, limit)
		if isCode(err, connect.CodeNotFound) {
			// The library was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		items := make([]any, len(movies))
		for i, m := range movies {
			items[i] = p.movieItem(m, iq.full)
		}
		return items, total, nil
	}
}

func (p *plugin) seriesSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]any, int, error) {
		series, total, err := p.listSeries(ctx, iq, offset, limit)
		if isCode(err, connect.CodeNotFound) {
			// The library was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		items := make([]any, len(series))
		for i, s := range series {
			items[i] = p.seriesItem(s)
		}
		return items, total, nil
	}
}

// seasonSource lists seasons by series and number.
func (p *plugin) seasonSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]any, int, error) {
		res, err := p.library.ListSeasons(ctx, &libraryv1.ListSeasonsRequest{
			LibraryId: iq.libraryID,
			SeriesId:  iq.seriesID,
			Ids:       iq.ids,
		})
		if isCode(err, connect.CodeNotFound) {
			// The library or series was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		seasons := res.GetSeasons()
		page := paginate(seasons, offset, limit)
		items := make([]any, len(page))
		for i, s := range page {
			items[i] = p.seasonItem(s)
		}
		return items, len(seasons), nil
	}
}

func (p *plugin) episodeSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]any, int, error) {
		episodes, total, err := p.listEpisodes(ctx, iq, offset, limit)
		if isCode(err, connect.CodeNotFound) {
			// The library, series or season was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		items := make([]any, len(episodes))
		for i, e := range episodes {
			items[i] = p.episodeItem(e, iq.full)
		}
		return items, total, nil
	}
}

// paginate returns the page of s from offset of at most limit elements
// (zero means no limit).
func paginate[T any](s []T, offset, limit int) []T {
	if offset >= len(s) {
		return nil
	}
	s = s[offset:]
	if limit > 0 && limit < len(s) {
		s = s[:limit]
	}
	return s
}

// latest is an item of /Items/Latest with the time it was added.
type latest struct {
	added time.Time
	item  BaseItemDto
}

// latestItems lists the items added last, of a library or of every
// library: movies, and episodes grouped by series (a series with several
// new episodes is listed instead of them, unless groupItems is false).
// The response is an array, not a query result.
func (p *plugin) latestItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	q := lowerQuery(r)
	limit, ok := intParam(q, "limit")
	if !ok {
		badRequest(w, r, "invalid limit")
		return
	}
	if limit == 0 {
		limit = defaultLatestLimit
	}
	items := []BaseItemDto{}
	if p.library == nil || isTrue(q["isplayed"]) {
		writeJSON(w, http.StatusOK, items)
		return
	}

	var lib *libraryv1.Library
	if parent := q["parentid"]; parent != "" {
		scope, _, ok := p.parentScope(w, r, parent)
		if !ok {
			return
		}
		if scope.library == nil {
			writeJSON(w, http.StatusOK, items)
			return
		}
		lib = scope.library
	}
	full := wantsField(q, "MediaSources") || wantsField(q, "MediaStreams")

	var all []latest
	if lib.GetMediaType() != mediaTypeTVShows && includesType(q, "Movie") {
		movies, _, err := p.listMovies(r.Context(), itemQuery{libraryID: lib.GetId(), sortBy: "added", descending: true}, 0, limit)
		if err != nil && !isCode(err, connect.CodeNotFound) {
			p.internalError(w, r, "list movies", err)
			return
		}
		for _, m := range movies {
			all = append(all, latest{m.GetCreatedAt().AsTime(), p.movieItem(m, full)})
		}
	}
	if lib.GetMediaType() != mediaTypeMovies && (includesType(q, "Episode") || contains(q["includeitemtypes"], "Series")) {
		group := q["groupitems"] == "" || isTrue(q["groupitems"])
		episodes, err := p.latestEpisodes(r.Context(), lib.GetId(), limit, group, full)
		if err != nil {
			p.internalError(w, r, "list episodes", err)
			return
		}
		all = append(all, episodes...)
	}

	slices.SortStableFunc(all, func(a, b latest) int { return b.added.Compare(a.added) })
	for _, l := range paginate(all, 0, limit) {
		items = append(items, l.item)
	}
	writeJSON(w, http.StatusOK, items)
}

// latestEpisodes lists the episodes added last to a library (or to every
// library), newest first. When group is set, the new episodes of a series
// are replaced by the series, if there are several.
func (p *plugin) latestEpisodes(ctx context.Context, lib string, limit int, group, full bool) ([]latest, error) {
	window := limit
	if group {
		window = min(limit*latestEpisodesWindow, 500)
	}
	episodes, _, err := p.listEpisodes(ctx, itemQuery{libraryID: lib, episodeSortBy: "added", descending: true}, 0, window)
	if isCode(err, connect.CodeNotFound) {
		return nil, nil // the library was removed meanwhile
	}
	if err != nil {
		return nil, err
	}
	if !group {
		out := make([]latest, len(episodes))
		for i, e := range episodes {
			out[i] = latest{e.GetCreatedAt().AsTime(), p.episodeItem(e, full)}
		}
		return out, nil
	}

	var order []string // the series, newest first
	bySeries := map[string][]*libraryv1.Episode{}
	for _, e := range episodes {
		id := e.GetSeriesId()
		if _, seen := bySeries[id]; !seen {
			if len(order) == limit {
				continue
			}
			order = append(order, id)
		}
		bySeries[id] = append(bySeries[id], e)
	}
	var grouped []string
	for _, id := range order {
		if len(bySeries[id]) > 1 {
			grouped = append(grouped, id)
		}
	}
	series := map[string]*libraryv1.Series{}
	if len(grouped) > 0 {
		found, _, err := p.listSeries(ctx, itemQuery{ids: grouped}, 0, 0)
		if err != nil {
			return nil, err
		}
		for _, s := range found {
			series[s.GetId()] = s
		}
	}
	out := make([]latest, 0, len(order))
	for _, id := range order {
		newest := bySeries[id][0]
		if s, ok := series[id]; ok {
			out = append(out, latest{newest.GetCreatedAt().AsTime(), p.seriesItem(s)})
		} else {
			// A single episode, or a series removed meanwhile.
			out = append(out, latest{newest.GetCreatedAt().AsTime(), p.episodeItem(newest, full)})
		}
	}
	return out, nil
}

// item returns an item; movies and episodes come with their media sources.
func (p *plugin) item(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	ref, ok := p.lookupItem(w, r)
	if !ok {
		return
	}
	var item BaseItemDto
	switch {
	case ref.movie != nil:
		item = p.movieItem(ref.movie, true)
	case ref.episode != nil:
		item = p.episodeItem(ref.episode, true)
	case ref.series != nil:
		item = p.seriesItem(ref.series)
	case ref.season != nil:
		item = p.seasonItem(ref.season)
	default:
		item = p.libraryItem(ref.library)
	}
	writeJSON(w, http.StatusOK, item)
}

// ancestors returns the parents of an item, nearest first: the season,
// the series and the library of an episode; the library of a movie. A
// library has no parents visible to the user.
func (p *plugin) ancestors(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	ref, ok := p.lookupItem(w, r)
	if !ok {
		return
	}
	var seasonID, seriesID, libraryID string
	switch {
	case ref.movie != nil:
		libraryID = ref.movie.GetLibraryId()
	case ref.episode != nil:
		seasonID, seriesID, libraryID = ref.episode.GetSeasonId(), ref.episode.GetSeriesId(), ref.episode.GetLibraryId()
	case ref.season != nil:
		seriesID, libraryID = ref.season.GetSeriesId(), ref.season.GetLibraryId()
	case ref.series != nil:
		libraryID = ref.series.GetLibraryId()
	}

	// Parents removed meanwhile are skipped.
	ctx := r.Context()
	parents := []BaseItemDto{}
	if seasonID != "" {
		res, err := p.library.GetSeason(ctx, &libraryv1.GetSeasonRequest{LibraryId: libraryID, SeriesId: seriesID, Id: seasonID})
		if err == nil {
			parents = append(parents, p.seasonItem(res.GetSeason()))
		} else if !isCode(err, connect.CodeNotFound) {
			p.internalError(w, r, "get season", err)
			return
		}
	}
	if seriesID != "" {
		res, err := p.library.GetSeries(ctx, &libraryv1.GetSeriesRequest{LibraryId: libraryID, Id: seriesID})
		if err == nil {
			parents = append(parents, p.seriesItem(res.GetSeries()))
		} else if !isCode(err, connect.CodeNotFound) {
			p.internalError(w, r, "get series", err)
			return
		}
	}
	if libraryID != "" {
		res, err := p.library.GetLibrary(ctx, &libraryv1.GetLibraryRequest{Id: libraryID})
		if err == nil {
			parents = append(parents, p.libraryItem(res.GetLibrary()))
		} else if !isCode(err, connect.CodeNotFound) {
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
	if _, ok := p.lookupItem(w, r); !ok {
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
	if _, ok := p.lookupItem(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []any{}})
}

// queryFiltersLegacy lists the years of the items of the query, so that
// the library can be filtered by them. The items have no genres, tags or
// ratings until metadata.
func (p *plugin) queryFiltersLegacy(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	years, _, ok := p.filterSources(w, r)
	if !ok {
		return
	}
	slices.Sort(years)
	writeJSON(w, http.StatusOK, QueryFiltersLegacy{
		Genres:          []string{},
		Tags:            []string{},
		OfficialRatings: []string{},
		Years:           slices.Compact(years),
	})
}

// queryFilters lists the audio and subtitle languages of the items of the
// query. The items have no genres or tags until metadata.
func (p *plugin) queryFilters(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	_, media, ok := p.filterSources(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, QueryFilters{
		Genres:            []NameGuidPair{},
		Tags:              []string{},
		AudioLanguages:    streamLanguages(media, "audio"),
		SubtitleLanguages: streamLanguages(media, "subtitle"),
	})
}

// filterSources returns what the filters of /Items/Filters and
// /Items/Filters2 are computed from: the known years and the media info
// of the items of the parentId folder, or of every library. The streams of
// a series are the ones of its episodes.
func (p *plugin) filterSources(w http.ResponseWriter, r *http.Request) ([]int, []*libraryv1.MediaInfo, bool) {
	q := lowerQuery(r)
	years := []int{}
	if p.library == nil {
		return years, nil, true
	}
	if mediaTypes := q["mediatypes"]; mediaTypes != "" && !contains(mediaTypes, "Video") {
		return years, nil, true
	}
	var (
		mediaType string
		scope     itemQuery
	)
	if parent := q["parentid"]; parent != "" {
		ref, folder, ok := p.parentScope(w, r, parent)
		if !ok {
			return nil, nil, false
		}
		switch {
		case !folder:
			return years, nil, true // e.g. a movie: it has no children
		case ref.library != nil:
			mediaType, scope.libraryID = ref.library.GetMediaType(), ref.library.GetId()
		case ref.series != nil:
			mediaType, scope.seriesID = mediaTypeTVShows, ref.series.GetId()
		default:
			mediaType, scope.seasonID = mediaTypeTVShows, ref.season.GetId()
		}
	}

	ctx := r.Context()
	var media []*libraryv1.MediaInfo
	if mediaType != mediaTypeTVShows && includesType(q, "Movie") {
		movies, _, err := p.listMovies(ctx, scope, 0, 0)
		if isCode(err, connect.CodeNotFound) {
			return years, media, true // the library was removed meanwhile
		}
		if err != nil {
			p.internalError(w, r, "list movies", err)
			return nil, nil, false
		}
		for _, m := range movies {
			if m.GetYear() > 0 {
				years = append(years, int(m.GetYear()))
			}
			media = append(media, m.GetMediaInfo())
		}
	}
	if mediaType != mediaTypeMovies && (includesType(q, "Series") || includesType(q, "Episode")) {
		episodes, _, err := p.listEpisodes(ctx, scope, 0, 0)
		if isCode(err, connect.CodeNotFound) {
			return years, media, true // the parent was removed meanwhile
		}
		if err != nil {
			p.internalError(w, r, "list episodes", err)
			return nil, nil, false
		}
		for _, e := range episodes {
			if e.GetSeriesYear() > 0 {
				years = append(years, int(e.GetSeriesYear()))
			}
			media = append(media, e.GetMediaInfo())
		}
	}
	return years, media, true
}

// streamLanguages lists the known languages of the streams of type typ,
// by name. The value is the language code, which /Items filters on.
func streamLanguages(media []*libraryv1.MediaInfo, typ string) []NameValuePair {
	languages := []NameValuePair{}
	seen := map[string]bool{}
	for _, info := range media {
		if info == nil {
			continue
		}
		for _, s := range info.GetStreams() {
			code := strings.ToLower(s.GetLanguage())
			if s.GetType() != typ || code == "" || code == "und" || seen[code] {
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

// emptyItems answers the item queries that have no results yet (intros,
// similar items, next up).
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

// itemsParams are the query parameters /Items understands, or knowingly
// ignores because they only shape the response (images, user data...).
var itemsParams = map[string]bool{
	"startindex": true, "limit": true, "ids": true, "years": true, "audiolanguages": true,
	"subtitlelanguages": true, "sortby": true, "sortorder": true, "fields": true, "recursive": true,
	"parentid": true, "includeitemtypes": true, "excludeitemtypes": true, "mediatypes": true,
	"filters": true, "userid": true, "api_key": true, "apikey": true, "enableimages": true,
	"enableimagetypes": true, "imagetypelimit": true, "enableuserdata": true,
	"enabletotalrecordcount": true,
}

// ignoredParams lists, sorted, the parameters of an /Items query that the
// plugin does not handle: what a client asks that is silently ignored.
func ignoredParams(q map[string]string) []string {
	var out []string
	for k := range q {
		if !itemsParams[k] && !slices.Contains(unsupportedBoolParams, k) && !slices.Contains(unsupportedValueParams, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

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

// Filters no item matches yet: user state (until watchstate) or metadata
// (until metadata).
var (
	unsupportedBoolParams  = []string{"isfavorite", "isplayed", "isresumable", "islocked", "isplaceholder", "ismissing"}
	unsupportedValueParams = []string{
		"searchterm", "genres", "genreids", "personids", "person", "persontypes", "studios",
		"studioids", "tags", "officialratings", "artistids", "albumids", "namestartswith",
		"namestartswithorgreater", "namelessthan", "minpremieredate", "maxpremieredate",
	}
)

// unsupportedFilter returns the filter of the query that no item matches
// yet, if any: user state (until watchstate) or metadata (until
// metadata).
func unsupportedFilter(q map[string]string) string {
	for _, name := range unsupportedBoolParams {
		if isTrue(q[name]) {
			return name
		}
	}
	for _, f := range list(q["filters"]) {
		switch f {
		case "isfavorite", "isplayed", "isresumable", "likes", "dislikes", "isfavoriteorlikes", "isfolder":
			return "filters=" + f
		}
	}
	for _, name := range unsupportedValueParams {
		if q[name] != "" {
			return name
		}
	}
	if mediaTypes := q["mediatypes"]; mediaTypes != "" && !contains(mediaTypes, "Video") {
		return "mediatypes=" + mediaTypes
	}
	return ""
}

// movieSort maps the Jellyfin sort (the first of sortBy and sortOrder) to
// the library sort of movies and series.
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

// episodeSort maps the Jellyfin sort to the library sort of episodes: by
// series, season and number, unless sorted by the date added.
func episodeSort(sortBy string) string {
	if fields := list(sortBy); len(fields) > 0 && fields[0] == "datecreated" {
		return "added"
	}
	return "number"
}

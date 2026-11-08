package jellyfin

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	library "github.com/lukerops/mserver/internal/modules/library/client"
	libraryrouter "github.com/lukerops/mserver/internal/modules/library/router"
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

// itemRef is an item of the catalog: exactly one field is set, unless the
// item does not exist.
type itemRef struct {
	movie   *libraryrouter.MovieV1
	episode *libraryrouter.EpisodeV1
	series  *libraryrouter.SeriesV1
	season  *libraryrouter.SeasonV1
	library *libraryrouter.LibraryV1
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
	if m, err := p.library.GetMovie(ctx, id); err == nil {
		return itemRef{movie: &m}, nil
	} else if !errors.Is(err, library.ErrNotFound) {
		return itemRef{}, err
	}
	if e, err := p.library.GetEpisode(ctx, uuid.Nil, uuid.Nil, id); err == nil {
		return itemRef{episode: &e}, nil
	} else if !errors.Is(err, library.ErrNotFound) {
		return itemRef{}, err
	}
	if s, err := p.library.GetSeries(ctx, id); err == nil {
		return itemRef{series: &s}, nil
	} else if !errors.Is(err, library.ErrNotFound) {
		return itemRef{}, err
	}
	if s, err := p.library.GetSeason(ctx, uuid.Nil, id); err == nil {
		return itemRef{season: &s}, nil
	} else if !errors.Is(err, library.ErrNotFound) {
		return itemRef{}, err
	}
	if l, err := p.library.GetLibrary(ctx, id); err == nil {
		return itemRef{library: &l}, nil
	} else if !errors.Is(err, library.ErrNotFound) {
		return itemRef{}, err
	}
	return itemRef{}, nil
}

// lookupItem finds the item of the itemId path value. When there is none,
// it answers 404 and returns false.
func (p *plugin) lookupItem(w http.ResponseWriter, r *http.Request) (itemRef, bool) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil || p.library == nil {
		http.NotFound(w, r)
		return itemRef{}, false
	}
	ref, err := p.findItem(r.Context(), id)
	if err != nil {
		p.internalError(w, r, "find item", err)
		return itemRef{}, false
	}
	if !ref.exists() {
		http.NotFound(w, r)
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
		http.Error(w, "invalid parentId", http.StatusBadRequest)
		return itemRef{}, false, false
	}
	ref, err := p.findItem(r.Context(), id)
	if err != nil {
		p.internalError(w, r, "find item", err)
		return itemRef{}, false, false
	}
	return ref, ref.isFolder(), true
}

// itemQuery holds the filters and the sort of /Items.
type itemQuery struct {
	ids                     []uuid.UUID
	years                   []int
	audio, subtitle         []string
	sortBy, episodeSortBy   string
	descending              bool
	full                    bool
	libraryID               uuid.UUID
	seriesID, seasonID      uuid.UUID
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
		http.Error(w, "invalid startIndex or limit", http.StatusBadRequest)
		return
	}
	empty := BaseItemDtoQueryResult{Items: []any{}, StartIndex: start}
	if p.library == nil || unsupportedFilter(q) {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	var iq itemQuery
	if s := q["ids"]; s != "" {
		for part := range strings.SplitSeq(s, ",") {
			id, err := uuid.Parse(strings.TrimSpace(part))
			if err != nil {
				http.Error(w, "invalid ids", http.StatusBadRequest)
				return
			}
			iq.ids = append(iq.ids, id)
		}
	}
	for _, s := range list(q["years"]) {
		y, err := strconv.Atoi(s)
		if err != nil || y <= 0 {
			http.Error(w, "invalid years", http.StatusBadRequest)
			return
		}
		iq.years = append(iq.years, y)
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
			iq.seasonID = scope.season.ID
			kinds = []string{"Episode"}
		case scope.series != nil:
			iq.seriesID = scope.series.ID
			kinds = []string{"Season", "Episode"}
		case scope.library.MediaType == mediaTypeTVShows:
			iq.libraryID = scope.library.ID
			kinds = []string{"Series", "Season", "Episode"}
		default:
			iq.libraryID = scope.library.ID
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
	for _, kind := range kinds {
		if !includesType(q, kind) {
			continue
		}
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
		page, err := p.library.ListMovies(ctx, libraryrouter.MovieQueryV1{
			LibraryID:         iq.libraryID,
			IDs:               iq.ids,
			Years:             iq.years,
			AudioLanguages:    iq.audio,
			SubtitleLanguages: iq.subtitle,
			SortBy:            iq.sortBy,
			Descending:        iq.descending,
			Offset:            offset,
			Limit:             limit,
		})
		if err != nil {
			return nil, 0, err
		}
		items := make([]any, len(page.Movies))
		for i, m := range page.Movies {
			items[i] = p.movieItem(m, iq.full)
		}
		return items, page.Total, nil
	}
}

func (p *plugin) seriesSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]any, int, error) {
		page, err := p.library.ListSeries(ctx, libraryrouter.SeriesQueryV1{
			LibraryID:         iq.libraryID,
			IDs:               iq.ids,
			Years:             iq.years,
			AudioLanguages:    iq.audio,
			SubtitleLanguages: iq.subtitle,
			SortBy:            iq.sortBy,
			Descending:        iq.descending,
			Offset:            offset,
			Limit:             limit,
		})
		if err != nil {
			return nil, 0, err
		}
		items := make([]any, len(page.Series))
		for i, s := range page.Series {
			items[i] = p.seriesItem(s)
		}
		return items, page.Total, nil
	}
}

// seasonSource lists seasons by series and number.
func (p *plugin) seasonSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]any, int, error) {
		seasons, err := p.library.ListSeasons(ctx, iq.seriesID, libraryrouter.SeasonQueryV1{
			LibraryID: iq.libraryID,
			IDs:       iq.ids,
		})
		if errors.Is(err, library.ErrNotFound) {
			// The series was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
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
		page, err := p.library.ListEpisodes(ctx, iq.seriesID, iq.seasonID, libraryrouter.EpisodeQueryV1{
			LibraryID:         iq.libraryID,
			IDs:               iq.ids,
			AudioLanguages:    iq.audio,
			SubtitleLanguages: iq.subtitle,
			SortBy:            iq.episodeSortBy,
			Descending:        iq.descending,
			Offset:            offset,
			Limit:             limit,
		})
		if errors.Is(err, library.ErrNotFound) {
			// The series or season was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		items := make([]any, len(page.Episodes))
		for i, e := range page.Episodes {
			items[i] = p.episodeItem(e, iq.full)
		}
		return items, page.Total, nil
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
		http.Error(w, "invalid limit", http.StatusBadRequest)
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

	var lib libraryrouter.LibraryV1
	if parent := q["parentid"]; parent != "" {
		scope, _, ok := p.parentScope(w, r, parent)
		if !ok {
			return
		}
		if scope.library == nil {
			writeJSON(w, http.StatusOK, items)
			return
		}
		lib = *scope.library
	}
	full := wantsField(q, "MediaSources") || wantsField(q, "MediaStreams")

	var all []latest
	if lib.MediaType != mediaTypeTVShows && includesType(q, "Movie") {
		page, err := p.library.ListMovies(r.Context(), libraryrouter.MovieQueryV1{
			LibraryID: lib.ID, SortBy: "added", Descending: true, Limit: limit,
		})
		if err != nil {
			p.internalError(w, r, "list movies", err)
			return
		}
		for _, m := range page.Movies {
			all = append(all, latest{m.CreatedAt, p.movieItem(m, full)})
		}
	}
	if lib.MediaType != mediaTypeMovies && (includesType(q, "Episode") || contains(q["includeitemtypes"], "Series")) {
		group := q["groupitems"] == "" || isTrue(q["groupitems"])
		episodes, err := p.latestEpisodes(r.Context(), lib.ID, limit, group, full)
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
func (p *plugin) latestEpisodes(ctx context.Context, lib uuid.UUID, limit int, group, full bool) ([]latest, error) {
	window := limit
	if group {
		window = min(limit*latestEpisodesWindow, 500)
	}
	page, err := p.library.ListEpisodes(ctx, uuid.Nil, uuid.Nil, libraryrouter.EpisodeQueryV1{
		LibraryID: lib, SortBy: "added", Descending: true, Limit: window,
	})
	if err != nil {
		return nil, err
	}
	if !group {
		out := make([]latest, len(page.Episodes))
		for i, e := range page.Episodes {
			out[i] = latest{e.CreatedAt, p.episodeItem(e, full)}
		}
		return out, nil
	}

	var order []uuid.UUID // the series, newest first
	bySeries := map[uuid.UUID][]libraryrouter.EpisodeV1{}
	for _, e := range page.Episodes {
		if _, seen := bySeries[e.SeriesID]; !seen {
			if len(order) == limit {
				continue
			}
			order = append(order, e.SeriesID)
		}
		bySeries[e.SeriesID] = append(bySeries[e.SeriesID], e)
	}
	var grouped []uuid.UUID
	for _, id := range order {
		if len(bySeries[id]) > 1 {
			grouped = append(grouped, id)
		}
	}
	series := map[uuid.UUID]libraryrouter.SeriesV1{}
	if len(grouped) > 0 {
		page, err := p.library.ListSeries(ctx, libraryrouter.SeriesQueryV1{IDs: grouped})
		if err != nil {
			return nil, err
		}
		for _, s := range page.Series {
			series[s.ID] = s
		}
	}
	out := make([]latest, 0, len(order))
	for _, id := range order {
		newest := bySeries[id][0]
		if s, ok := series[id]; ok {
			out = append(out, latest{newest.CreatedAt, p.seriesItem(s)})
		} else {
			// A single episode, or a series removed meanwhile.
			out = append(out, latest{newest.CreatedAt, p.episodeItem(newest, full)})
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
		item = p.movieItem(*ref.movie, true)
	case ref.episode != nil:
		item = p.episodeItem(*ref.episode, true)
	case ref.series != nil:
		item = p.seriesItem(*ref.series)
	case ref.season != nil:
		item = p.seasonItem(*ref.season)
	default:
		item = p.libraryItem(*ref.library)
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
	var seasonID, seriesID, libraryID uuid.UUID
	switch {
	case ref.movie != nil:
		libraryID = ref.movie.LibraryID
	case ref.episode != nil:
		seasonID, seriesID, libraryID = ref.episode.SeasonID, ref.episode.SeriesID, ref.episode.LibraryID
	case ref.season != nil:
		seriesID, libraryID = ref.season.SeriesID, ref.season.LibraryID
	case ref.series != nil:
		libraryID = ref.series.LibraryID
	}

	// Parents removed meanwhile are skipped.
	ctx := r.Context()
	parents := []BaseItemDto{}
	if seasonID != uuid.Nil {
		s, err := p.library.GetSeason(ctx, seriesID, seasonID)
		if err == nil {
			parents = append(parents, p.seasonItem(s))
		} else if !errors.Is(err, library.ErrNotFound) {
			p.internalError(w, r, "get season", err)
			return
		}
	}
	if seriesID != uuid.Nil {
		s, err := p.library.GetSeries(ctx, seriesID)
		if err == nil {
			parents = append(parents, p.seriesItem(s))
		} else if !errors.Is(err, library.ErrNotFound) {
			p.internalError(w, r, "get series", err)
			return
		}
	}
	if libraryID != uuid.Nil {
		l, err := p.library.GetLibrary(ctx, libraryID)
		if err == nil {
			parents = append(parents, p.libraryItem(l))
		} else if !errors.Is(err, library.ErrNotFound) {
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
func (p *plugin) filterSources(w http.ResponseWriter, r *http.Request) ([]int, []*libraryrouter.MediaInfoV1, bool) {
	q := lowerQuery(r)
	years := []int{}
	if p.library == nil {
		return years, nil, true
	}
	if mediaTypes := q["mediatypes"]; mediaTypes != "" && !contains(mediaTypes, "Video") {
		return years, nil, true
	}
	var (
		lib      libraryrouter.LibraryV1
		seriesID uuid.UUID
		seasonID uuid.UUID
	)
	if parent := q["parentid"]; parent != "" {
		scope, folder, ok := p.parentScope(w, r, parent)
		if !ok {
			return nil, nil, false
		}
		switch {
		case !folder:
			return years, nil, true // e.g. a movie: it has no children
		case scope.library != nil:
			lib = *scope.library
		case scope.series != nil:
			lib.MediaType, seriesID = mediaTypeTVShows, scope.series.ID
		default:
			lib.MediaType, seasonID = mediaTypeTVShows, scope.season.ID
		}
	}

	ctx := r.Context()
	var media []*libraryrouter.MediaInfoV1
	if lib.MediaType != mediaTypeTVShows && includesType(q, "Movie") {
		page, err := p.library.ListMovies(ctx, libraryrouter.MovieQueryV1{LibraryID: lib.ID})
		if err != nil {
			p.internalError(w, r, "list movies", err)
			return nil, nil, false
		}
		for _, m := range page.Movies {
			if m.Year > 0 {
				years = append(years, m.Year)
			}
			media = append(media, m.MediaInfo)
		}
	}
	if lib.MediaType != mediaTypeMovies && (includesType(q, "Series") || includesType(q, "Episode")) {
		page, err := p.library.ListEpisodes(ctx, seriesID, seasonID, libraryrouter.EpisodeQueryV1{
			LibraryID: lib.ID,
		})
		if errors.Is(err, library.ErrNotFound) {
			return years, media, true // the parent was removed meanwhile
		}
		if err != nil {
			p.internalError(w, r, "list episodes", err)
			return nil, nil, false
		}
		for _, e := range page.Episodes {
			if e.SeriesYear > 0 {
				years = append(years, e.SeriesYear)
			}
			media = append(media, e.MediaInfo)
		}
	}
	return years, media, true
}

// streamLanguages lists the known languages of the streams of type typ,
// by name. The value is the language code, which /Items filters on.
func streamLanguages(media []*libraryrouter.MediaInfoV1, typ string) []NameValuePair {
	languages := []NameValuePair{}
	seen := map[string]bool{}
	for _, info := range media {
		if info == nil {
			continue
		}
		for _, s := range info.Streams {
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
// item has yet: user state (until watchstate) or metadata (until
// metadata).
func unsupportedFilter(q map[string]string) bool {
	for _, name := range []string{"isfavorite", "isplayed", "isresumable", "islocked", "isplaceholder", "ismissing"} {
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

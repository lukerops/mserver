package service

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

// latestEpisodesWindow is how many recent episodes /Items/Latest groups
// by series, per item asked.
const latestEpisodesWindow = 5

// Media types of the libraries, which are also their Jellyfin collection
// types.
const (
	mediaTypeMovies  = "movies"
	mediaTypeTVShows = "tvshows"
)

// ItemQuery holds the filters and the sort of /Items. The IDs are UUIDs in
// the canonical form, as the library takes them. The type lists are lower
// case, as the Jellyfin query parameters.
type ItemQuery struct {
	IDs               []string
	Years             []int32
	AudioLanguages    []string
	SubtitleLanguages []string
	SortBy            string
	EpisodeSortBy     string
	Descending        bool
	Recursive         bool
	ParentID          string
	IncludeTypes      []string
	ExcludeTypes      []string
	Full              bool
	Start             int
	Limit             int
}

// LatestQuery is the query of /Items/Latest.
type LatestQuery struct {
	ParentID     string
	Limit        int
	Group        bool
	IncludeTypes []string
	ExcludeTypes []string
	Full         bool
}

// FilterQuery is the query of /Items/Filters and /Items/Filters2.
type FilterQuery struct {
	ParentID     string
	MediaTypes   []string
	IncludeTypes []string
	ExcludeTypes []string
}

// EpisodeQuery is the query of /Shows/{seriesId}/Episodes.
type EpisodeQuery struct {
	SeriesID     uuid.UUID
	SeasonID     string
	SeasonNumber *int
	StartItemID  string
	Start        int
	Limit        int
	Full         bool
}

// itemQuery is the query as the library takes it.
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
type source func(ctx context.Context, offset, limit int) ([]Item, int, error)

// Items lists the children of a folder (a library, a series or a season),
// or its descendants when the query is recursive, or the items of every
// library.
func (s *Service) Items(ctx context.Context, q ItemQuery) (ItemQueryResult, error) {
	res := ItemQueryResult{Items: []Item{}, StartIndex: q.Start}
	if s.library == nil {
		return res, nil
	}

	iq := itemQuery{
		ids:                     q.IDs,
		years:                   q.Years,
		audio:                   q.AudioLanguages,
		subtitle:                q.SubtitleLanguages,
		sortBy:                  q.SortBy,
		episodeSortBy:           q.EpisodeSortBy,
		descending:              q.Descending,
		full:                    q.Full,
		hasLanguageOrYearFilter: q.Years != nil || q.AudioLanguages != nil || q.SubtitleLanguages != nil,
	}

	var kinds []string
	switch parent := q.ParentID; {
	case parent != "":
		scope, folder, err := s.parentScope(ctx, parent)
		if err != nil {
			return ItemQueryResult{}, err
		}
		if !folder {
			return res, nil // e.g. a movie: it has no children
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
	case iq.ids == nil && !q.Recursive:
		// The children of the root of the user are its views.
		items, err := s.Libraries(ctx)
		if err != nil {
			return ItemQueryResult{}, err
		}
		return ItemQueryResult{Items: items, TotalRecordCount: len(items), StartIndex: q.Start}, nil
	default:
		kinds = []string{"Movie", "Series", "Season", "Episode"}
	}
	// Without types to include, a query lists the children of the folder,
	// unless it is recursive or asks for items by id.
	if len(q.IncludeTypes) == 0 && !q.Recursive && iq.ids == nil {
		kinds = kinds[:1]
	}

	var sources []source
	for _, kind := range kinds {
		if !includesType(q.IncludeTypes, q.ExcludeTypes, kind) {
			continue
		}
		switch kind {
		case "Movie":
			sources = append(sources, s.movieSource(iq))
		case "Series":
			sources = append(sources, s.seriesSource(iq))
		case "Season":
			// Seasons have no years or streams of their own.
			if !iq.hasLanguageOrYearFilter {
				sources = append(sources, s.seasonSource(iq))
			}
		case "Episode":
			// Episodes have no year until metadata.
			if iq.years == nil {
				sources = append(sources, s.episodeSource(iq))
			}
		}
	}
	return collect(ctx, sources, q.Start, q.Limit)
}

// collect concatenates the sources, returning the page from start of at
// most limit items (zero means no limit).
func collect(ctx context.Context, sources []source, start, limit int) (ItemQueryResult, error) {
	res := ItemQueryResult{Items: []Item{}, StartIndex: start}
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

func (s *Service) movieSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]Item, int, error) {
		movies, total, err := s.listMovies(ctx, iq, offset, limit)
		if isCode(err, connect.CodeNotFound) {
			// The library was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		items := make([]Item, len(movies))
		for i, m := range movies {
			items[i] = movieItem(m, iq.full)
		}
		return items, total, nil
	}
}

func (s *Service) seriesSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]Item, int, error) {
		series, total, err := s.listSeries(ctx, iq, offset, limit)
		if isCode(err, connect.CodeNotFound) {
			// The library was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		items := make([]Item, len(series))
		for i, ser := range series {
			items[i] = seriesItem(ser)
		}
		return items, total, nil
	}
}

// seasonSource lists seasons by series and number.
func (s *Service) seasonSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]Item, int, error) {
		res, err := s.library.ListSeasons(ctx, &libraryv1.ListSeasonsRequest{
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
		items := make([]Item, len(page))
		for i, season := range page {
			items[i] = seasonItem(season)
		}
		return items, len(seasons), nil
	}
}

func (s *Service) episodeSource(iq itemQuery) source {
	return func(ctx context.Context, offset, limit int) ([]Item, int, error) {
		episodes, total, err := s.listEpisodes(ctx, iq, offset, limit)
		if isCode(err, connect.CodeNotFound) {
			// The library, series or season was removed meanwhile.
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		items := make([]Item, len(episodes))
		for i, e := range episodes {
			items[i] = episodeItem(e, iq.full)
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
	item  Item
}

// Latest lists the items added last, of a library or of every library:
// movies, and episodes grouped by series.
func (s *Service) Latest(ctx context.Context, q LatestQuery) ([]Item, error) {
	items := []Item{}
	if s.library == nil {
		return items, nil
	}

	var lib *libraryv1.Library
	if q.ParentID != "" {
		scope, _, err := s.parentScope(ctx, q.ParentID)
		if err != nil {
			return nil, err
		}
		if scope.library == nil {
			return items, nil
		}
		lib = scope.library
	}

	var all []latest
	if lib.GetMediaType() != mediaTypeTVShows && includesType(q.IncludeTypes, q.ExcludeTypes, "Movie") {
		movies, _, err := s.listMovies(ctx, itemQuery{libraryID: lib.GetId(), sortBy: "added", descending: true}, 0, q.Limit)
		if err != nil && !isCode(err, connect.CodeNotFound) {
			return nil, err
		}
		for _, m := range movies {
			all = append(all, latest{m.GetCreatedAt().AsTime(), movieItem(m, q.Full)})
		}
	}
	if lib.GetMediaType() != mediaTypeMovies && (includesType(q.IncludeTypes, q.ExcludeTypes, "Episode") || includesType(q.IncludeTypes, q.ExcludeTypes, "Series")) {
		episodes, err := s.latestEpisodes(ctx, lib.GetId(), q.Limit, q.Group, q.Full)
		if err != nil {
			return nil, err
		}
		all = append(all, episodes...)
	}

	slices.SortStableFunc(all, func(a, b latest) int { return b.added.Compare(a.added) })
	for _, l := range paginate(all, 0, q.Limit) {
		items = append(items, l.item)
	}
	return items, nil
}

// latestEpisodes lists the episodes added last to a library (or to every
// library), newest first. When group is set, the new episodes of a series
// are replaced by the series, if there are several.
func (s *Service) latestEpisodes(ctx context.Context, lib string, limit int, group, full bool) ([]latest, error) {
	window := limit
	if group {
		window = min(limit*latestEpisodesWindow, 500)
	}
	episodes, _, err := s.listEpisodes(ctx, itemQuery{libraryID: lib, episodeSortBy: "added", descending: true}, 0, window)
	if isCode(err, connect.CodeNotFound) {
		return nil, nil // the library was removed meanwhile
	}
	if err != nil {
		return nil, err
	}
	if !group {
		out := make([]latest, len(episodes))
		for i, e := range episodes {
			out[i] = latest{e.GetCreatedAt().AsTime(), episodeItem(e, full)}
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
		found, _, err := s.listSeries(ctx, itemQuery{ids: grouped}, 0, 0)
		if err != nil {
			return nil, err
		}
		for _, ser := range found {
			series[ser.GetId()] = ser
		}
	}
	out := make([]latest, 0, len(order))
	for _, id := range order {
		newest := bySeries[id][0]
		if ser, ok := series[id]; ok {
			out = append(out, latest{newest.GetCreatedAt().AsTime(), seriesItem(ser)})
		} else {
			// A single episode, or a series removed meanwhile.
			out = append(out, latest{newest.GetCreatedAt().AsTime(), episodeItem(newest, full)})
		}
	}
	return out, nil
}

// Ancestors returns the parents of an item, nearest first: the season, the
// series and the library of an episode; the library of a movie. A library
// has no parents visible to the user.
func (s *Service) Ancestors(ctx context.Context, id uuid.UUID) ([]Item, error) {
	if s.library == nil {
		return nil, errNotFound("item not found: no library module")
	}
	ref, err := s.findItem(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ref.exists() {
		return nil, errNotFound("item not found")
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
	parents := []Item{}
	if seasonID != "" {
		res, err := s.library.GetSeason(ctx, &libraryv1.GetSeasonRequest{LibraryId: libraryID, SeriesId: seriesID, Id: seasonID})
		if err == nil {
			parents = append(parents, seasonItem(res.GetSeason()))
		} else if !isCode(err, connect.CodeNotFound) {
			return nil, err
		}
	}
	if seriesID != "" {
		res, err := s.library.GetSeries(ctx, &libraryv1.GetSeriesRequest{LibraryId: libraryID, Id: seriesID})
		if err == nil {
			parents = append(parents, seriesItem(res.GetSeries()))
		} else if !isCode(err, connect.CodeNotFound) {
			return nil, err
		}
	}
	if libraryID != "" {
		res, err := s.library.GetLibrary(ctx, &libraryv1.GetLibraryRequest{Id: libraryID})
		if err == nil {
			parents = append(parents, libraryItem(res.GetLibrary()))
		} else if !isCode(err, connect.CodeNotFound) {
			return nil, err
		}
	}
	return parents, nil
}

// QueryFiltersLegacy lists the years of the items of the query. The items
// have no genres, tags or ratings until metadata.
func (s *Service) QueryFiltersLegacy(ctx context.Context, q FilterQuery) (QueryFiltersLegacy, error) {
	years, _, err := s.filterSources(ctx, q)
	if err != nil {
		return QueryFiltersLegacy{}, err
	}
	slices.Sort(years)
	return QueryFiltersLegacy{
		Genres:          []string{},
		Tags:            []string{},
		OfficialRatings: []string{},
		Years:           slices.Compact(years),
	}, nil
}

// QueryFilters lists the audio and subtitle languages of the items of the
// query. The items have no genres or tags until metadata.
func (s *Service) QueryFilters(ctx context.Context, q FilterQuery) (QueryFilters, error) {
	_, media, err := s.filterSources(ctx, q)
	if err != nil {
		return QueryFilters{}, err
	}
	return QueryFilters{
		Genres:            []NameGuidPair{},
		Tags:              []string{},
		AudioLanguages:    streamLanguages(media, "audio"),
		SubtitleLanguages: streamLanguages(media, "subtitle"),
	}, nil
}

// filterSources returns what the filters of /Items/Filters and
// /Items/Filters2 are computed from: the known years and the media info
// of the items of the parentId folder, or of every library.
func (s *Service) filterSources(ctx context.Context, q FilterQuery) ([]int, []*libraryv1.MediaInfo, error) {
	years := []int{}
	if s.library == nil {
		return years, nil, nil
	}
	if len(q.MediaTypes) > 0 && !slices.Contains(q.MediaTypes, "video") {
		return years, nil, nil
	}
	var (
		mediaType string
		scope     itemQuery
	)
	if q.ParentID != "" {
		ref, folder, err := s.parentScope(ctx, q.ParentID)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case !folder:
			return years, nil, nil // e.g. a movie: it has no children
		case ref.library != nil:
			mediaType, scope.libraryID = ref.library.GetMediaType(), ref.library.GetId()
		case ref.series != nil:
			mediaType, scope.seriesID = mediaTypeTVShows, ref.series.GetId()
		default:
			mediaType, scope.seasonID = mediaTypeTVShows, ref.season.GetId()
		}
	}

	var media []*libraryv1.MediaInfo
	if mediaType != mediaTypeTVShows && includesType(q.IncludeTypes, q.ExcludeTypes, "Movie") {
		movies, _, err := s.listMovies(ctx, scope, 0, 0)
		if isCode(err, connect.CodeNotFound) {
			return years, media, nil // the library was removed meanwhile
		}
		if err != nil {
			return nil, nil, err
		}
		for _, m := range movies {
			if m.GetYear() > 0 {
				years = append(years, int(m.GetYear()))
			}
			media = append(media, m.GetMediaInfo())
		}
	}
	if mediaType != mediaTypeMovies && (includesType(q.IncludeTypes, q.ExcludeTypes, "Series") || includesType(q.IncludeTypes, q.ExcludeTypes, "Episode")) {
		episodes, _, err := s.listEpisodes(ctx, scope, 0, 0)
		if isCode(err, connect.CodeNotFound) {
			return years, media, nil // the parent was removed meanwhile
		}
		if err != nil {
			return nil, nil, err
		}
		for _, e := range episodes {
			if e.GetSeriesYear() > 0 {
				years = append(years, int(e.GetSeriesYear()))
			}
			media = append(media, e.GetMediaInfo())
		}
	}
	return years, media, nil
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

// includesType reports whether the type filters accept items of type typ.
// The lists are lower case.
func includesType(include, exclude []string, typ string) bool {
	typ = strings.ToLower(typ)
	if len(include) > 0 && !slices.Contains(include, typ) {
		return false
	}
	return !slices.Contains(exclude, typ)
}

package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// defaultLatestLimit is the number of items of /Items/Latest when the
// client sends no limit, as in Jellyfin.
const defaultLatestLimit = 20

// userViews lists the libraries ("My Media").
func (h *Handler) userViews(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	items, err := h.svc.Libraries(r.Context())
	if err != nil {
		h.serviceError(w, r, "list libraries", err)
		return
	}
	writeJSON(w, http.StatusOK, service.ItemQueryResult{Items: items, TotalRecordCount: len(items)})
}

// items lists the children of a folder (a library, a series or a season),
// or its descendants when the query is recursive, or the items of every
// library. Filters that depend on the user state (favorites, played...) or
// on metadata the library does not have yet (genres, people...) match
// nothing.
func (h *Handler) items(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	q := lowerQuery(r)
	start, okStart := intParam(q, "startindex")
	limit, okLimit := intParam(q, "limit")
	if !okStart || !okLimit {
		badRequest(w, r, "invalid startIndex or limit")
		return
	}
	if f := unsupportedFilter(q); f != "" {
		h.log.DebugContext(r.Context(), "jellyfin: items: unsupported filter, empty result", slog.String("filter", f))
		writeJSON(w, http.StatusOK, service.ItemQueryResult{Items: []service.Item{}, StartIndex: start})
		return
	}

	ids, ok := itemIDs(w, r, q["ids"])
	if !ok {
		return
	}
	years, ok := itemYears(w, r, q["years"])
	if !ok {
		return
	}
	sortBy, descending := movieSort(q["sortby"], q["sortorder"])

	res, err := h.svc.Items(r.Context(), service.ItemQuery{
		IDs:               ids,
		Years:             years,
		AudioLanguages:    list(q["audiolanguages"]),
		SubtitleLanguages: list(q["subtitlelanguages"]),
		SortBy:            sortBy,
		EpisodeSortBy:     episodeSort(q["sortby"]),
		Descending:        descending,
		Recursive:         isTrue(q["recursive"]),
		ParentID:          q["parentid"],
		IncludeTypes:      list(q["includeitemtypes"]),
		ExcludeTypes:      list(q["excludeitemtypes"]),
		Full:              wantsField(q, "MediaSources") || wantsField(q, "MediaStreams"),
		Start:             start,
		Limit:             limit,
	})
	if err != nil {
		h.serviceError(w, r, "list items", err)
		return
	}
	signItems(res.Items, requestAuthorization(r).Token)
	writeJSON(w, http.StatusOK, res)
}

// itemIDs parses the "ids" parameter into canonical UUIDs. When there is
// an invalid one, it answers 400 and returns false.
func itemIDs(w http.ResponseWriter, r *http.Request, s string) ([]string, bool) {
	if s == "" {
		return nil, true
	}
	var ids []string
	for part := range strings.SplitSeq(s, ",") {
		id, err := uuid.Parse(strings.TrimSpace(part))
		if err != nil {
			badRequest(w, r, "invalid ids")
			return nil, false
		}
		ids = append(ids, id.String())
	}
	return ids, true
}

// itemYears parses the "years" parameter. When there is an invalid one, it
// answers 400 and returns false.
func itemYears(w http.ResponseWriter, r *http.Request, s string) ([]int32, bool) {
	if s == "" {
		return nil, true
	}
	var years []int32
	for _, part := range list(s) {
		y, err := strconv.Atoi(part)
		if err != nil || y <= 0 {
			badRequest(w, r, "invalid years")
			return nil, false
		}
		years = append(years, int32(y))
	}
	return years, true
}

// latestItems lists the items added last, of a library or of every
// library: movies, and episodes grouped by series. The response is an
// array, not a query result.
func (h *Handler) latestItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
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
	if isTrue(q["isplayed"]) {
		writeJSON(w, http.StatusOK, []service.Item{})
		return
	}
	items, err := h.svc.Latest(r.Context(), service.LatestQuery{
		ParentID:     q["parentid"],
		Limit:        limit,
		Group:        q["groupitems"] == "" || isTrue(q["groupitems"]),
		IncludeTypes: list(q["includeitemtypes"]),
		ExcludeTypes: list(q["excludeitemtypes"]),
		Full:         wantsField(q, "MediaSources") || wantsField(q, "MediaStreams"),
	})
	if err != nil {
		h.serviceError(w, r, "list latest", err)
		return
	}
	token := requestAuthorization(r).Token
	for i := range items {
		signItem(&items[i], token)
	}
	writeJSON(w, http.StatusOK, items)
}

// item returns an item; movies and episodes come with their media sources.
func (h *Handler) item(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.Item(r.Context(), id)
	if err != nil {
		h.serviceError(w, r, "find item", err)
		return
	}
	signItem(&item, requestAuthorization(r).Token)
	writeJSON(w, http.StatusOK, item)
}

// ancestors returns the parents of an item, nearest first.
func (h *Handler) ancestors(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	parents, err := h.svc.Ancestors(r.Context(), id)
	if err != nil {
		h.serviceError(w, r, "ancestors", err)
		return
	}
	writeJSON(w, http.StatusOK, parents)
}

// themeMedia answers that the item has no theme songs or videos.
func (h *Handler) themeMedia(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.Item(r.Context(), id)
	if err != nil {
		h.serviceError(w, r, "find item", err)
		return
	}
	empty := service.ThemeMediaResult{Items: []service.Item{}, OwnerId: item.Id}
	writeJSON(w, http.StatusOK, service.AllThemeMediaResult{
		ThemeVideosResult:     empty,
		ThemeSongsResult:      empty,
		SoundtrackSongsResult: empty,
	})
}

// collections answers that the item belongs to no collection (box set).
func (h *Handler) collections(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	if _, err := h.svc.Item(r.Context(), id); err != nil {
		h.serviceError(w, r, "find item", err)
		return
	}
	writeJSON(w, http.StatusOK, service.ItemQueryResult{Items: []service.Item{}})
}

// queryFiltersLegacy lists the years of the items of the query, so that the
// library can be filtered by them. The items have no genres, tags or
// ratings until metadata.
func (h *Handler) queryFiltersLegacy(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	filters, err := h.svc.QueryFiltersLegacy(r.Context(), filterQuery(r))
	if err != nil {
		h.serviceError(w, r, "query filters", err)
		return
	}
	writeJSON(w, http.StatusOK, filters)
}

// queryFilters lists the audio and subtitle languages of the items of the
// query. The items have no genres or tags until metadata.
func (h *Handler) queryFilters(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	filters, err := h.svc.QueryFilters(r.Context(), filterQuery(r))
	if err != nil {
		h.serviceError(w, r, "query filters", err)
		return
	}
	writeJSON(w, http.StatusOK, filters)
}

func filterQuery(r *http.Request) service.FilterQuery {
	q := lowerQuery(r)
	return service.FilterQuery{
		ParentID:     q["parentid"],
		MediaTypes:   list(q["mediatypes"]),
		IncludeTypes: list(q["includeitemtypes"]),
		ExcludeTypes: list(q["excludeitemtypes"]),
	}
}

// itemID parses the itemId path value. When it is invalid, it answers 404
// and returns false.
func itemID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil {
		notFound(w, r, "invalid itemId")
		return uuid.Nil, false
	}
	return id, true
}

// emptyItems answers the item queries that have no results yet (intros,
// similar items, next up).
func (h *Handler) emptyItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, service.ItemQueryResult{Items: []service.Item{}})
}

// emptyItemList answers the item lists that are always empty yet (special
// features, local trailers).
func (h *Handler) emptyItemList(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, []service.Item{})
}

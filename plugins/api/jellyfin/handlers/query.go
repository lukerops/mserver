package handlers

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

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

func wantsField(q map[string]string, field string) bool {
	return contains(q["fields"], field)
}

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

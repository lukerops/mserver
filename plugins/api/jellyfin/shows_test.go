package jellyfin_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	"github.com/lukerops/mserver/internal/clients"
)

// startWithShows serves the plugin with a series library holding two
// series: one with a season, a double episode and a special, and an anime
// in absolute numbering.
func startWithShows(t *testing.T) (client, string, *libraryv1.Library) {
	t.Helper()
	return startWithLibrary(t, "Séries", "tvshows", []string{
		"Breaking Bad (2008)/Season 01/Breaking Bad - S01E01 - Pilot.mkv",
		"Breaking Bad (2008)/Season 01/Breaking Bad - S01E02E03.mkv",
		"Breaking Bad (2008)/Specials/Breaking Bad - S00E01 - Minisode.mkv",
		"One Piece/[Group] One Piece - 105 [1080p].mkv",
	}, func(c clients.Clients) (bool, any) {
		res, err := c.Library.ListEpisodes(t.Context(), &libraryv1.ListEpisodesRequest{})
		if err != nil {
			t.Fatal(err)
		}
		episodes := res.GetEpisodes()
		for _, e := range episodes {
			if e.GetMediaInfo() == nil {
				return false, episodes
			}
		}
		return len(episodes) == 4, episodes
	})
}

// showItem is an item with the fields of series, seasons and episodes.
type showItem struct {
	item
	IndexNumber, IndexNumberEnd, ParentIndexNumber *int
	ChildCount, RecursiveItemCount                 *int
	SeriesId, SeriesName, SeasonId, SeasonName     string
}

type showsResult struct {
	Items            []showItem
	TotalRecordCount int
	StartIndex       int
}

func names(items []showItem) []string {
	var out []string
	for _, i := range items {
		out = append(out, i.Type+":"+i.Name)
	}
	return out
}

func ptr(n *int) string {
	if n == nil {
		return "nil"
	}
	return fmt.Sprint(*n)
}

func TestSeriesNavigation(t *testing.T) {
	c, token, lib := startWithShows(t)
	libID := hex(lib.GetId())

	var views showsResult
	c.expect("GET", "/UserViews", token, nil, &views, http.StatusOK)
	if len(views.Items) != 1 || views.Items[0].CollectionType != "tvshows" || views.Items[0].Name != "Séries" {
		t.Fatalf("views = %+v", views)
	}

	// The library page, as jellyfin-web asks for it.
	params := url.Values{
		"SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "IncludeItemTypes": {"Series"},
		"Recursive": {"true"}, "Fields": {"PrimaryImageAspectRatio"}, "StartIndex": {"0"}, "ParentId": {libID},
	}
	var series showsResult
	c.expect("GET", "/Items?"+params.Encode(), token, nil, &series, http.StatusOK)
	if got := names(series.Items); series.TotalRecordCount != 2 ||
		!slices.Equal(got, []string{"Series:Breaking Bad", "Series:One Piece"}) {
		t.Fatalf("series = %v (%+v)", got, series)
	}
	bb := series.Items[0]
	if !bb.IsFolder || bb.ParentId != libID || bb.ProductionYear != 2008 || ptr(bb.ChildCount) != "2" || ptr(bb.RecursiveItemCount) != "3" {
		t.Errorf("series item = %+v", bb)
	}
	// The children of the library are its series.
	var children showsResult
	c.expect("GET", "/Items?ParentId="+libID, token, nil, &children, http.StatusOK)
	if got := names(children.Items); !slices.Equal(got, []string{"Series:Breaking Bad", "Series:One Piece"}) {
		t.Errorf("children of the library = %v", got)
	}

	// The series page.
	var item showItem
	c.expect("GET", "/Items/"+bb.Id, token, nil, &item, http.StatusOK)
	if item.Type != "Series" || item.Name != "Breaking Bad" {
		t.Errorf("series = %+v", item)
	}
	var seasons showsResult
	c.expect("GET", "/Shows/"+bb.Id+"/Seasons?userId=x&Fields=ItemCounts", token, nil, &seasons, http.StatusOK)
	if got := names(seasons.Items); seasons.TotalRecordCount != 2 || !slices.Equal(got, []string{"Season:Specials", "Season:Season 1"}) {
		t.Fatalf("seasons = %v", got)
	}
	season1 := seasons.Items[1]
	if ptr(season1.IndexNumber) != "1" || season1.SeriesId != bb.Id || season1.ParentId != bb.Id ||
		season1.SeriesName != "Breaking Bad" || ptr(season1.ChildCount) != "2" {
		t.Errorf("season = %+v", season1)
	}
	c.expect("GET", "/Shows/"+bb.Id+"/Seasons?isSpecialSeason=false", token, nil, &seasons, http.StatusOK)
	if got := names(seasons.Items); !slices.Equal(got, []string{"Season:Season 1"}) {
		t.Errorf("seasons without specials = %v", got)
	}
	var next showsResult
	c.expect("GET", "/Shows/NextUp?SeriesId="+bb.Id+"&Limit=1", token, nil, &next, http.StatusOK)
	if next.Items == nil || len(next.Items) != 0 {
		t.Errorf("next up = %+v", next)
	}
	c.expect("GET", "/Shows/"+hex(uuid.NewString())+"/Seasons", token, nil, nil, http.StatusNotFound)
	c.expect("GET", "/Shows/"+bb.Id+"/Seasons", "", nil, nil, http.StatusUnauthorized)

	// The season page.
	var episodes showsResult
	c.expect("GET", "/Shows/"+bb.Id+"/Episodes?seasonId="+season1.Id+"&Fields=MediaSources", token, nil, &episodes, http.StatusOK)
	if got := names(episodes.Items); episodes.TotalRecordCount != 2 || !slices.Equal(got, []string{"Episode:Pilot", "Episode:Breaking Bad - S01E02E03"}) {
		t.Fatalf("episodes = %v", got)
	}
	double := episodes.Items[1]
	if ptr(double.IndexNumber) != "2" || ptr(double.IndexNumberEnd) != "3" || ptr(double.ParentIndexNumber) != "1" ||
		double.SeasonId != season1.Id || double.SeasonName != "Season 1" || double.SeriesId != bb.Id ||
		double.ParentId != season1.Id || len(double.MediaSources) != 1 {
		t.Errorf("double episode = %+v", double)
	}
	for query, want := range map[string]string{
		"":                          "[Episode:Minisode Episode:Pilot Episode:Breaking Bad - S01E02E03]",
		"?season=0":                 "[Episode:Minisode]",
		"?season=7":                 "[]",
		"?startItemId=" + double.Id: "[Episode:Breaking Bad - S01E02E03]",
		"?startIndex=1&limit=1":     "[Episode:Pilot]",
		"?isMissing=true":           "[]",
	} {
		var res showsResult
		c.expect("GET", "/Shows/"+bb.Id+"/Episodes"+query, token, nil, &res, http.StatusOK)
		if got := fmt.Sprint(names(res.Items)); got != want && !(want == "[]" && res.Items != nil && len(res.Items) == 0) {
			t.Errorf("/Shows/{id}/Episodes%s = %s, want %s", query, got, want)
		}
	}
	var seasonChildren showsResult
	c.expect("GET", "/Items?ParentId="+season1.Id, token, nil, &seasonChildren, http.StatusOK)
	if seasonChildren.TotalRecordCount != 2 {
		t.Errorf("children of the season = %v", names(seasonChildren.Items))
	}
	var seriesChildren showsResult
	c.expect("GET", "/Items?ParentId="+bb.Id, token, nil, &seriesChildren, http.StatusOK)
	if got := names(seriesChildren.Items); !slices.Equal(got, []string{"Season:Specials", "Season:Season 1"}) {
		t.Errorf("children of the series = %v", got)
	}

	// The episode page.
	var episode showItem
	c.expect("GET", "/Items/"+double.Id, token, nil, &episode, http.StatusOK)
	if episode.Type != "Episode" || episode.MediaType != "Video" || len(episode.MediaSources) != 1 ||
		len(episode.MediaStreams) != 2 || episode.RunTimeTicks == 0 || ptr(episode.IndexNumberEnd) != "3" {
		t.Errorf("episode = %+v", episode)
	}
	var ancestors []showItem
	c.expect("GET", "/Items/"+double.Id+"/Ancestors", token, nil, &ancestors, http.StatusOK)
	if got := names(ancestors); !slices.Equal(got, []string{"Season:Season 1", "Series:Breaking Bad", "CollectionFolder:Séries"}) {
		t.Errorf("ancestors = %v", got)
	}

	// The anime is in a single season, by absolute number.
	var anime showsResult
	c.expect("GET", "/Shows/"+series.Items[1].Id+"/Episodes", token, nil, &anime, http.StatusOK)
	if len(anime.Items) != 1 || ptr(anime.Items[0].IndexNumber) != "105" || ptr(anime.Items[0].ParentIndexNumber) != "1" {
		t.Errorf("anime = %+v", anime)
	}

	// Several types at once are paged together.
	var mixed showsResult
	c.expect("GET", "/Items?ParentId="+libID+"&Recursive=true&IncludeItemTypes=Series,Episode&StartIndex=1&Limit=2", token, nil, &mixed, http.StatusOK)
	if got := names(mixed.Items); mixed.TotalRecordCount != 6 || !slices.Equal(got, []string{"Series:One Piece", "Episode:Minisode"}) {
		t.Errorf("series and episodes = %v (total %d)", got, mixed.TotalRecordCount)
	}
	var allEpisodes showsResult
	c.expect("GET", "/Items?ParentId="+libID+"&Recursive=true&IncludeItemTypes=Episode&audioLanguages=eng", token, nil, &allEpisodes, http.StatusOK)
	if allEpisodes.TotalRecordCount != 4 {
		t.Errorf("episodes of the library = %v", names(allEpisodes.Items))
	}

	// "Recently Added": the new episodes of a series are grouped.
	var latest []showItem
	c.expect("GET", "/Items/Latest?ParentId="+libID+"&Limit=16", token, nil, &latest, http.StatusOK)
	if got := names(latest); len(got) != 2 || !slices.Contains(got, "Series:Breaking Bad") ||
		!slices.Contains(got, "Episode:One Piece - 105") && !slices.Contains(got, "Episode:[Group] One Piece - 105 [1080p]") {
		t.Errorf("latest = %v", got)
	}
	c.expect("GET", "/Items/Latest?ParentId="+libID+"&GroupItems=false", token, nil, &latest, http.StatusOK)
	if len(latest) != 4 {
		t.Errorf("latest ungrouped = %v", names(latest))
	}

	var filters struct {
		AudioLanguages []struct{ Name, Value string }
	}
	c.expect("GET", "/Items/Filters2?parentId="+libID+"&includeItemTypes=Series", token, nil, &filters, http.StatusOK)
	if fmt.Sprint(filters.AudioLanguages) != "[{English eng}]" {
		t.Errorf("filters = %+v", filters)
	}
	var legacyFilters struct{ Years []int }
	c.expect("GET", "/Items/Filters?parentId="+libID+"&includeItemTypes=Series", token, nil, &legacyFilters, http.StatusOK)
	if fmt.Sprint(legacyFilters.Years) != "[2008]" {
		t.Errorf("legacy filters = %+v", legacyFilters)
	}
}

package jellyfin_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/internal/clients"
)

// startWithMovies serves the plugin on identity, storage and library
// modules, with a movie library holding two analyzed movies. It returns
// the client, the authorization of a logged in user and the library.
func startWithMovies(t *testing.T) (client, string, *libraryv1.Library) {
	t.Helper()
	return startWithLibrary(t, "Filmes", "movies",
		[]string{"Heat (1995).mkv", "Heat (1995).en.srt", "Alien (1979)/Alien.mkv"},
		func(c clients.Clients) (bool, any) {
			res, err := c.Library.ListMovies(t.Context(), &libraryv1.ListMoviesRequest{})
			if err != nil {
				t.Fatal(err)
			}
			movies := res.GetMovies()
			// Alien has no subtitle; Heat has one, matched after the movies
			// are cataloged.
			return len(movies) == 2 && movies[0].GetMediaInfo() != nil && movies[1].GetMediaInfo() != nil &&
				len(movies[1].GetExternalSubtitles()) == 1, movies
		})
}

// startWithLibrary serves the plugin on identity, storage and library
// modules, with a library of a media type holding files (real videos for
// .mkv, subtitles otherwise). It waits until ready reports that the files
// are cataloged (and returns what to report otherwise). It returns the
// client, the authorization of a logged in user and the library.
func startWithLibrary(t *testing.T, name, mediaType string, files []string, ready func(clients.Clients) (bool, any)) (client, string, *libraryv1.Library) {
	t.Helper()
	t.Setenv("MSERVER_STORAGE_SCAN_INTERVAL", "0")
	t.Setenv("MSERVER_FFPROBE_PATH", ffprobePath(t))
	ctx := t.Context()

	e := newEnv(t)
	e.startIdentity()
	e.startCatalog()
	c := e.clients

	media := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for _, f := range files {
		p := filepath.Join(media, f)
		if filepath.Ext(p) == ".mkv" {
			writeMovie(t, p)
		} else if err := os.WriteFile(p, []byte("1\n00:00:00,000 --> 00:00:01,000\nHello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	loc, err := c.Storage.CreateLocation(ctx, &storagev1.CreateLocationRequest{
		Driver: "local", Options: map[string]string{"dir": media},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Storage.ScanLocation(ctx, &storagev1.ScanLocationRequest{Id: loc.GetLocation().GetId()}); err != nil {
		t.Fatal(err)
	}
	created, err := c.Library.CreateLibrary(ctx, &libraryv1.CreateLibraryRequest{
		Name: name, MediaType: mediaType, LocationIds: []string{loc.GetLocation().GetId()},
	})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		ok, state := ready(c)
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("files not cataloged: %+v", state)
		}
		time.Sleep(50 * time.Millisecond)
	}

	srv := serve(t, c)
	jc := client{t: t, base: srv.URL, modules: c}

	var auth struct{ AccessToken string }
	jc.expect("POST", "/Users/AuthenticateByName", authHeader,
		map[string]string{"Username": "admin#lucas", "Pw": password}, &auth, http.StatusOK)
	return jc, authHeader + `, Token="` + auth.AccessToken + `"`, created.GetLibrary()
}

type mediaStream struct {
	Type, Codec, DisplayTitle, Language string
	DeliveryMethod, DeliveryUrl         string
	Index, Width, Height, Channels      int
	IsDefault, IsExternal               bool
	SupportsExternalStream              bool
}

type item struct {
	Id, Name, ServerId, Type, CollectionType, ParentId, Container, SortName, MediaType string
	IsFolder                                                                           bool
	RunTimeTicks                                                                       int64
	ProductionYear                                                                     int
	DateCreated                                                                        time.Time
	UserData                                                                           struct {
		Key        string
		Played     bool
		IsFavorite bool
	}
	MediaSources []struct {
		Id, Protocol, Container string
		Size, Bitrate           int64
		RunTimeTicks            int64
		SupportsDirectPlay      bool
		DefaultAudioStreamIndex *int
		MediaStreams            []mediaStream
	}
	MediaStreams []mediaStream
}

type itemsResult struct {
	Items            []item
	TotalRecordCount int
	StartIndex       int
}

func hex(id string) string { return strings.ReplaceAll(id, "-", "") }

func TestMovieNavigation(t *testing.T) {
	c, token, lib := startWithMovies(t)
	libID := hex(lib.GetId())

	c.expect("GET", "/Items", "", nil, nil, http.StatusUnauthorized)

	// "My Media".
	var views itemsResult
	c.expect("GET", "/UserViews?userId=x", token, nil, &views, http.StatusOK)
	if views.TotalRecordCount != 1 || len(views.Items) != 1 {
		t.Fatalf("views = %+v", views)
	}
	if v := views.Items[0]; v.Id != libID || v.Name != "Filmes" || v.Type != "CollectionFolder" ||
		v.CollectionType != "movies" || !v.IsFolder || v.ServerId == "" {
		t.Errorf("view = %+v", v)
	}

	// The library page, as jellyfin-web asks for it.
	params := url.Values{
		"SortBy": {"SortName,ProductionYear"}, "SortOrder": {"Ascending"},
		"IncludeItemTypes": {"Movie"}, "Recursive": {"true"},
		"Fields": {"PrimaryImageAspectRatio,MediaSourceCount"}, "StartIndex": {"0"},
		"Limit": {"1"}, "ParentId": {libID},
	}
	var page itemsResult
	c.expect("GET", "/Items?"+params.Encode(), token, nil, &page, http.StatusOK)
	if page.TotalRecordCount != 2 || len(page.Items) != 1 || page.Items[0].Name != "Alien" ||
		page.Items[0].MediaSources != nil || page.Items[0].ParentId != libID {
		t.Fatalf("page = %+v", page)
	}
	params.Set("StartIndex", "1")
	params.Set("Fields", "MediaSources")
	c.expect("GET", "/Items?"+params.Encode(), token, nil, &page, http.StatusOK)
	if page.StartIndex != 1 || len(page.Items) != 1 || page.Items[0].Name != "Heat" || len(page.Items[0].MediaSources) != 1 {
		t.Fatalf("second page = %+v", page)
	}
	heat := page.Items[0]

	// The queries that depend on what does not exist yet match nothing.
	for _, query := range []string{
		"ParentId=" + libID + "&Filters=IsResumable&Recursive=true",
		"ParentId=" + libID + "&IsFavorite=true&Recursive=true",
		"ParentId=" + libID + "&IncludeItemTypes=Series&Recursive=true",
		"ParentId=" + heat.Id,
		"Recursive=true&SearchTerm=alien",
	} {
		var none itemsResult
		c.expect("GET", "/Items?"+query, token, nil, &none, http.StatusOK)
		if none.Items == nil || len(none.Items) != 0 || none.TotalRecordCount != 0 {
			t.Errorf("/Items?%s = %+v", query, none)
		}
	}
	var all itemsResult
	c.expect("GET", "/Items?recursive=true&includeItemTypes=Movie&ids="+heat.Id, token, nil, &all, http.StatusOK)
	if all.TotalRecordCount != 1 || all.Items[0].Id != heat.Id {
		t.Errorf("by ids = %+v", all)
	}

	// "Recently Added".
	var latest []item
	c.expect("GET", "/Items/Latest?Limit=16&ParentId="+libID+"&Fields=PrimaryImageAspectRatio", token, nil, &latest, http.StatusOK)
	if len(latest) != 2 || latest[0].DateCreated.Before(latest[1].DateCreated) {
		t.Fatalf("latest = %+v", latest)
	}
	c.expect("GET", "/Items/Latest?Limit=1", token, nil, &latest, http.StatusOK)
	if len(latest) != 1 {
		t.Fatalf("latest of every library = %+v", latest)
	}

	// The legacy routes with the user in the path (jellyfin-tizen).
	var legacyUser = "/Users/" + hex(uuid.NewString())
	c.expect("GET", legacyUser+"/Items?recursive=true&includeItemTypes=Movie&ids="+heat.Id, token, nil, &all, http.StatusOK)
	if all.TotalRecordCount != 1 || all.Items[0].Id != heat.Id {
		t.Errorf("legacy by ids = %+v", all)
	}
	c.expect("GET", legacyUser+"/Items/Latest?Limit=16&ParentId="+libID, token, nil, &latest, http.StatusOK)
	if len(latest) != 2 {
		t.Errorf("legacy latest = %+v", latest)
	}
	var legacyResume itemsResult
	c.expect("GET", legacyUser+"/Items/Resume?Limit=12&Recursive=true&MediaTypes=Video", token, nil, &legacyResume, http.StatusOK)
	if legacyResume.Items == nil || len(legacyResume.Items) != 0 {
		t.Errorf("legacy resume = %+v", legacyResume)
	}
	var legacyViews itemsResult
	c.expect("GET", legacyUser+"/Views", token, nil, &legacyViews, http.StatusOK)
	if len(legacyViews.Items) != 1 {
		t.Errorf("legacy views = %+v", legacyViews)
	}
	var legacyIntros itemsResult
	c.expect("GET", legacyUser+"/Items/"+heat.Id+"/Intros", token, nil, &legacyIntros, http.StatusOK)
	if legacyIntros.Items == nil || legacyIntros.TotalRecordCount != 0 {
		t.Errorf("legacy intros = %+v", legacyIntros)
	}

	// The movie page.
	var movie item
	c.expect("GET", "/Items/"+heat.Id+"?userId=x", token, nil, &movie, http.StatusOK)
	if movie.Type != "Movie" || movie.Name != "Heat" || movie.ProductionYear != 1995 ||
		movie.RunTimeTicks < movieMinDurationMs*10_000 || movie.Container != "mkv" || movie.MediaType != "Video" ||
		movie.UserData.Key != movie.Id || len(movie.MediaSources) != 1 || len(movie.MediaStreams) != 3 {
		t.Fatalf("movie = %+v", movie)
	}
	src := movie.MediaSources[0]
	if src.Id != movie.Id || src.Protocol != "File" || src.RunTimeTicks != movie.RunTimeTicks ||
		src.Size <= 0 || src.Bitrate <= 0 || !src.SupportsDirectPlay ||
		src.DefaultAudioStreamIndex == nil || *src.DefaultAudioStreamIndex != 1 {
		t.Errorf("media source = %+v", src)
	}
	video, audio := movie.MediaStreams[0], movie.MediaStreams[1]
	if video.Type != "Video" || video.DisplayTitle != "1080p H264 SDR" || video.Width != movieWidth {
		t.Errorf("video = %+v", video)
	}
	if audio.Type != "Audio" || audio.DisplayTitle != "English - AAC - Stereo - Default" || audio.Index != 1 {
		t.Errorf("audio = %+v", audio)
	}

	// Heat (1995).en.srt is an external subtitle, after the embedded streams.
	// Its DeliveryUrl carries the token, which clients fetch it with.
	_, rawToken, _ := strings.Cut(token, `Token="`)
	rawToken = strings.TrimSuffix(rawToken, `"`)
	subtitle := movie.MediaStreams[2]
	if subtitle.Type != "Subtitle" || subtitle.Codec != "srt" || subtitle.Index != 2 || subtitle.Language != "eng" ||
		!subtitle.IsExternal || !subtitle.SupportsExternalStream || subtitle.DeliveryMethod != "External" ||
		subtitle.DeliveryUrl != "/Videos/"+movie.Id+"/"+movie.Id+"/Subtitles/2/0/Stream.srt?ApiKey="+url.QueryEscape(rawToken) ||
		subtitle.DisplayTitle != "English - SRT - External" {
		t.Errorf("subtitle = %+v", subtitle)
	}
	if got := movie.MediaSources[0].MediaStreams; len(got) != 3 || got[2] != subtitle {
		t.Errorf("media source streams = %+v", got)
	}

	var folder item
	c.expect("GET", "/Items/"+libID, token, nil, &folder, http.StatusOK)
	if folder.Type != "CollectionFolder" || folder.Id != libID {
		t.Errorf("library item = %+v", folder)
	}
	c.expect("GET", "/Items/"+hex(uuid.NewString()), token, nil, nil, http.StatusNotFound)

	// The legacy route jellyfin-web loads the movie page from.
	var legacy item
	c.expect("GET", "/Users/"+hex(uuid.NewString())+"/Items/"+heat.Id, token, nil, &legacy, http.StatusOK)
	if legacy.Id != heat.Id || legacy.Type != "Movie" || len(legacy.MediaSources) != 1 {
		t.Errorf("legacy item = %+v", legacy)
	}
	c.expect("GET", "/Users/"+hex(uuid.NewString())+"/Items/"+hex(uuid.NewString()), token, nil, nil, http.StatusNotFound)

	var ancestors []item
	c.expect("GET", "/Items/"+heat.Id+"/Ancestors", token, nil, &ancestors, http.StatusOK)
	if len(ancestors) != 1 || ancestors[0].Id != libID || ancestors[0].Type != "CollectionFolder" {
		t.Errorf("ancestors = %+v", ancestors)
	}
	c.expect("GET", "/Items/"+libID+"/Ancestors", token, nil, &ancestors, http.StatusOK)
	if ancestors == nil || len(ancestors) != 0 {
		t.Errorf("library ancestors = %+v", ancestors)
	}
	c.expect("GET", "/Items/"+hex(uuid.NewString())+"/Ancestors", token, nil, nil, http.StatusNotFound)

	var theme struct {
		ThemeVideosResult, ThemeSongsResult, SoundtrackSongsResult *struct {
			Items            []item
			TotalRecordCount int
			OwnerId          string
		}
	}
	c.expect("GET", "/Items/"+heat.Id+"/ThemeMedia?userId=x&inheritFromParent=true&sortBy=Random", token, nil, &theme, http.StatusOK)
	if v, s := theme.ThemeVideosResult, theme.ThemeSongsResult; v == nil || s == nil || theme.SoundtrackSongsResult == nil ||
		v.Items == nil || len(v.Items) != 0 || v.OwnerId != heat.Id || s.Items == nil || len(s.Items) != 0 {
		t.Errorf("theme media = %+v", theme)
	}
	c.expect("GET", "/Items/"+hex(uuid.NewString())+"/ThemeMedia", token, nil, nil, http.StatusNotFound)

	var intros itemsResult
	c.expect("GET", "/Items/"+heat.Id+"/Intros", token, nil, &intros, http.StatusOK)
	if intros.Items == nil || intros.TotalRecordCount != 0 {
		t.Errorf("intros = %+v", intros)
	}
	var resume itemsResult
	c.expect("GET", "/UserItems/Resume?ParentId="+libID, token, nil, &resume, http.StatusOK)
	if resume.Items == nil || len(resume.Items) != 0 {
		t.Errorf("resume = %+v", resume)
	}

	// Nothing to filter by and no studios or collections until metadata.
	var studios itemsResult
	c.expect("GET", "/Studios?parentId="+libID+"&fields=DateCreated&includeItemTypes=Movie&enableTotalRecordCount=false", token, nil, &studios, http.StatusOK)
	if studios.Items == nil || len(studios.Items) != 0 {
		t.Errorf("studios = %+v", studios)
	}
	var legacyFilters struct {
		Genres, Tags, OfficialRatings []string
		Years                         []int
	}
	c.expect("GET", "/Items/Filters?userId=x&parentId="+libID+"&includeItemTypes=Movie", token, nil, &legacyFilters, http.StatusOK)
	if f := legacyFilters; f.Genres == nil || len(f.Genres) != 0 || f.Tags == nil || f.OfficialRatings == nil ||
		fmt.Sprint(f.Years) != "[1979 1995]" {
		t.Errorf("legacy filters = %+v", f)
	}
	c.expect("GET", "/Items/Filters?parentId="+heat.Id, token, nil, &legacyFilters, http.StatusOK)
	if legacyFilters.Years == nil || len(legacyFilters.Years) != 0 {
		t.Errorf("legacy filters of a movie = %+v", legacyFilters)
	}
	c.expect("GET", "/Items/Filters?includeItemTypes=Series", token, nil, &legacyFilters, http.StatusOK)
	if legacyFilters.Years == nil || len(legacyFilters.Years) != 0 {
		t.Errorf("legacy filters of series = %+v", legacyFilters)
	}
	var filters struct {
		Genres                            []any
		Tags                              []string
		AudioLanguages, SubtitleLanguages []struct{ Name, Value string }
	}
	c.expect("GET", "/Items/Filters2?userId=x&parentId="+libID+"&includeItemTypes=Movie", token, nil, &filters, http.StatusOK)
	if f := filters; f.Genres == nil || len(f.Genres) != 0 || f.Tags == nil || f.SubtitleLanguages == nil ||
		len(f.SubtitleLanguages) != 0 || fmt.Sprint(f.AudioLanguages) != "[{English eng}]" {
		t.Errorf("filters = %+v", f)
	}

	// /Items applies the filters.
	for query, want := range map[string]int{
		"years=1979":                    1,
		"years=1979&years=1995":         2,
		"years=1979,1995":               2,
		"years=2000":                    0,
		"audioLanguages=eng":            2,
		"audioLanguages=por":            0,
		"subtitleLanguages=eng":         0,
		"years=1995&audioLanguages=ENG": 1,
	} {
		var res itemsResult
		c.expect("GET", "/Items?parentId="+libID+"&includeItemTypes=Movie&"+query, token, nil, &res, http.StatusOK)
		if res.TotalRecordCount != want || len(res.Items) != want {
			t.Errorf("/Items?%s = %d items (total %d), want %d", query, len(res.Items), res.TotalRecordCount, want)
		}
	}
	c.expect("GET", "/Items?parentId="+libID+"&years=x", token, nil, nil, http.StatusBadRequest)
	var collections itemsResult
	c.expect("GET", "/Items/"+heat.Id+"/Collections?userId=x&fields=PrimaryImageAspectRatio", token, nil, &collections, http.StatusOK)
	if collections.Items == nil || len(collections.Items) != 0 {
		t.Errorf("collections = %+v", collections)
	}
	c.expect("GET", "/Items/"+hex(uuid.NewString())+"/Collections", token, nil, nil, http.StatusNotFound)
	c.expect("GET", "/Studios", "", nil, nil, http.StatusUnauthorized)
	c.expect("GET", "/Items/Filters", "", nil, nil, http.StatusUnauthorized)
	c.expect("GET", "/Items/Filters2", "", nil, nil, http.StatusUnauthorized)
}

package client_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/infra/ffmpeg/ffmpegtest"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/library"
	"github.com/lukerops/mserver/internal/modules/library/client"
	"github.com/lukerops/mserver/internal/modules/library/router"
	"github.com/lukerops/mserver/internal/modules/storage"
	storageclient "github.com/lukerops/mserver/internal/modules/storage/client"
	storagerouter "github.com/lukerops/mserver/internal/modules/storage/router"
)

func TestRoundTrip(t *testing.T) {
	ctx := t.Context()
	c := newServer(t)

	dir := t.TempDir()
	old := time.Now().Add(-time.Hour) // stable: analyzed right away
	for _, name := range []string{"Alien (1979)/Alien.mkv", "Alien (1979)/Alien.en.srt"} {
		p := filepath.Join(dir, name)
		writeMedia(t, p)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	loc, err := c.Storage.CreateLocation(ctx, storagerouter.CreateLocationRequestV1{
		Driver: "local", Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Storage.ScanLocation(ctx, loc.ID); err != nil {
		t.Fatal(err)
	}

	l := c.Library
	created, err := l.CreateLibrary(ctx, router.CreateLibraryRequestV1{
		Name: "Movies", MediaType: "movies", LocationIDs: []uuid.UUID{loc.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == uuid.Nil || created.Name != "Movies" || len(created.LocationIDs) != 1 {
		t.Fatalf("library = %+v", created)
	}
	if got, err := l.GetLibrary(ctx, created.ID); err != nil || got.ID != created.ID {
		t.Fatalf("GetLibrary = %+v, %v", got, err)
	}
	if libs, err := l.ListLibraries(ctx); err != nil || len(libs) != 1 {
		t.Fatalf("ListLibraries = %+v, %v", libs, err)
	}

	// The movie arrives from the storage events, then its analysis.
	var movie router.MovieV1
	deadline := time.Now().Add(10 * time.Second)
	for {
		page, err := l.ListMovies(ctx, created.ID, router.MovieQueryV1{SortBy: "added", Descending: true, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Movies) == 1 && page.Movies[0].MediaInfo != nil {
			if page.Total != 1 {
				t.Errorf("total = %d, want 1", page.Total)
			}
			movie = page.Movies[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("movies = %+v", page)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if movie.Title != "Alien" || movie.Year != 1979 || movie.LocationID != loc.ID ||
		movie.Path != "Alien (1979)/Alien.mkv" || movie.MediaInfo.DurationMs < ffmpegtest.MovieMinDurationMs ||
		len(movie.MediaInfo.Streams) != 2 || movie.MediaInfo.Streams[0].Width != ffmpegtest.MovieWidth {
		t.Errorf("movie = %+v (media %+v)", movie, movie.MediaInfo)
	}
	if got, err := l.GetMovie(ctx, created.ID, movie.ID); err != nil || got.ID != movie.ID || got.MediaInfo == nil {
		t.Errorf("GetMovie = %+v, %v", got, err)
	}
	if page, err := l.ListMovies(ctx, uuid.Nil, router.MovieQueryV1{IDs: []uuid.UUID{movie.ID, uuid.New()}}); err != nil || page.Total != 1 {
		t.Errorf("ListMovies by IDs = %+v, %v", page, err)
	}
	if page, err := l.ListMovies(ctx, uuid.Nil, router.MovieQueryV1{Years: []int{1979}, AudioLanguages: []string{"ENG"}}); err != nil || page.Total != 1 {
		t.Errorf("ListMovies by year and audio language = %+v, %v", page, err)
	}
	if page, err := l.ListMovies(ctx, uuid.Nil, router.MovieQueryV1{Years: []int{1980}}); err != nil || page.Total != 0 {
		t.Errorf("ListMovies by other year = %+v, %v", page, err)
	}
	if page, err := l.ListMovies(ctx, uuid.Nil, router.MovieQueryV1{SubtitleLanguages: []string{"eng"}}); err != nil || page.Total != 0 {
		t.Errorf("ListMovies by subtitle language = %+v, %v", page, err)
	}
	if err := l.ResyncLibrary(ctx, created.ID); err != nil {
		t.Errorf("ResyncLibrary: %v", err)
	}

	_, err = l.CreateLibrary(ctx, router.CreateLibraryRequestV1{
		Name: "Again", MediaType: "movies", LocationIDs: []uuid.UUID{loc.ID},
	})
	if !errors.Is(err, client.ErrAlreadyExists) {
		t.Errorf("location of another library: got %v, want ErrAlreadyExists", err)
	}
	_, err = l.CreateLibrary(ctx, router.CreateLibraryRequestV1{
		Name: "Unknown", MediaType: "movies", LocationIDs: []uuid.UUID{uuid.New()},
	})
	if !errors.Is(err, client.ErrInvalidArgument) {
		t.Errorf("unknown location: got %v, want ErrInvalidArgument", err)
	}
	if _, err := l.GetMovie(ctx, uuid.Nil, uuid.New()); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("unknown movie: got %v, want ErrNotFound", err)
	}
	if _, err := l.GetMovie(ctx, uuid.New(), movie.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("movie of another library: got %v, want ErrNotFound", err)
	}
	if _, err := l.ListMovies(ctx, uuid.New(), router.MovieQueryV1{}); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("movies of an unknown library: got %v, want ErrNotFound", err)
	}
	if _, err := l.ListMovies(ctx, uuid.Nil, router.MovieQueryV1{SortBy: "rating"}); !errors.Is(err, client.ErrInvalidArgument) {
		t.Errorf("unknown sort: got %v, want ErrInvalidArgument", err)
	}

	if err := l.DeleteLibrary(ctx, created.ID); err != nil {
		t.Fatalf("DeleteLibrary: %v", err)
	}
	if _, err := l.GetMovie(ctx, uuid.Nil, movie.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("movie of deleted library: got %v, want ErrNotFound", err)
	}
	if err := l.DeleteLibrary(ctx, created.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("deleted twice: got %v, want ErrNotFound", err)
	}
}

func TestSeriesRoundTrip(t *testing.T) {
	ctx := t.Context()
	c := newServer(t)

	dir := t.TempDir()
	old := time.Now().Add(-time.Hour) // stable: analyzed right away
	for _, name := range []string{
		"Firefly (2002)/Season 1/Firefly - S01E01 - Serenity.mkv",
		"Firefly (2002)/Season 1/Firefly - S01E02 - The Train Job.mkv",
		"Firefly (2002)/Specials/Firefly - S00E01 - Done the Impossible.mkv",
	} {
		p := filepath.Join(dir, name)
		writeMedia(t, p)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	loc, err := c.Storage.CreateLocation(ctx, storagerouter.CreateLocationRequestV1{
		Driver: "local", Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Storage.ScanLocation(ctx, loc.ID); err != nil {
		t.Fatal(err)
	}

	l := c.Library
	created, err := l.CreateLibrary(ctx, router.CreateLibraryRequestV1{
		Name: "Shows", MediaType: "tvshows", LocationIDs: []uuid.UUID{loc.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The episodes arrive from the storage events, then their analysis.
	var episodes router.EpisodeListV1
	deadline := time.Now().Add(10 * time.Second)
	for {
		episodes, err = l.ListEpisodes(ctx, created.ID, uuid.Nil, uuid.Nil, router.EpisodeQueryV1{})
		if err != nil {
			t.Fatal(err)
		}
		analyzed := 0
		for _, e := range episodes.Episodes {
			if e.MediaInfo != nil {
				analyzed++
			}
		}
		if analyzed == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("episodes = %+v", episodes)
		}
		time.Sleep(50 * time.Millisecond)
	}
	special, pilot := episodes.Episodes[0], episodes.Episodes[1]
	if special.SeasonNumber != 0 || pilot.SeasonNumber != 1 || pilot.Number == nil || *pilot.Number != 1 ||
		pilot.Title != "Serenity" || pilot.SeriesTitle != "Firefly" || pilot.SeriesYear != 2002 {
		t.Errorf("episodes = %+v, %+v", special, pilot)
	}

	series, err := l.ListSeries(ctx, created.ID, router.SeriesQueryV1{AudioLanguages: []string{"eng"}})
	if err != nil || series.Total != 1 || series.Series[0].SeasonCount != 2 || series.Series[0].EpisodeCount != 3 {
		t.Fatalf("ListSeries = %+v, %v", series, err)
	}
	if got, err := l.GetSeries(ctx, created.ID, pilot.SeriesID); err != nil || got.Title != "Firefly" {
		t.Errorf("GetSeries = %+v, %v", got, err)
	}
	seasons, err := l.ListSeasons(ctx, created.ID, pilot.SeriesID, router.SeasonQueryV1{})
	if err != nil || len(seasons) != 2 || seasons[1].ID != pilot.SeasonID || seasons[1].EpisodeCount != 2 {
		t.Errorf("ListSeasons = %+v, %v", seasons, err)
	}
	if got, err := l.GetSeason(ctx, uuid.Nil, pilot.SeriesID, pilot.SeasonID); err != nil || got.Number != 1 {
		t.Errorf("GetSeason = %+v, %v", got, err)
	}
	if _, err := l.GetSeason(ctx, uuid.Nil, uuid.New(), pilot.SeasonID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("season of another series: got %v, want ErrNotFound", err)
	}
	page, err := l.ListEpisodes(ctx, uuid.Nil, uuid.Nil, pilot.SeasonID, router.EpisodeQueryV1{Descending: true, Limit: 1})
	if err != nil || page.Total != 2 || page.Episodes[0].Title != "The Train Job" {
		t.Errorf("ListEpisodes of season = %+v, %v", page, err)
	}
	if got, err := l.GetEpisode(ctx, created.ID, pilot.SeriesID, pilot.SeasonID, pilot.ID); err != nil || got.FileID != pilot.FileID {
		t.Errorf("GetEpisode = %+v, %v", got, err)
	}
	if _, err := l.GetEpisode(ctx, uuid.Nil, uuid.Nil, uuid.Nil, uuid.New()); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("unknown episode: got %v, want ErrNotFound", err)
	}
	if _, err := l.ListEpisodes(ctx, uuid.Nil, uuid.Nil, uuid.Nil, router.EpisodeQueryV1{SortBy: "title"}); !errors.Is(err, client.ErrInvalidArgument) {
		t.Errorf("unknown sort: got %v, want ErrInvalidArgument", err)
	}

	if err := l.DeleteLibrary(ctx, created.ID); err != nil {
		t.Fatalf("DeleteLibrary: %v", err)
	}
	if _, err := l.GetSeries(ctx, uuid.Nil, pilot.SeriesID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("series of deleted library: got %v, want ErrNotFound", err)
	}
}

// newServer serves the storage and the library on the same server, as in
// cmd/server, and returns their clients.
func newServer(t *testing.T) clients.Clients {
	t.Helper()
	t.Setenv("MSERVER_STORAGE_SCAN_INTERVAL", "0")
	t.Setenv("MSERVER_FFPROBE_PATH", ffmpegtest.FFprobe(t))
	ctx := t.Context()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := clients.Clients{
		Storage: storageclient.New(srv.Client(), srv.URL),
		Library: client.New(srv.Client(), srv.URL),
	}
	bus := memory.New()
	lib, err := library.New(ctx, t.TempDir(), bus, bus, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	stor, err := storage.New(ctx, t.TempDir(), bus, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stor.Close() })
	for _, p := range []string{"libraries"} {
		mux.Handle("/v1/"+p, lib.Mux)
		mux.Handle("/v1/"+p+"/", lib.Mux)
	}
	mux.Handle("/", stor.Mux)
	return c
}

// writeMedia writes at p a real movie (.mkv) or subtitle (.srt).
func writeMedia(t *testing.T, p string) {
	t.Helper()
	if filepath.Ext(p) == ".mkv" {
		ffmpegtest.WriteMovie(t, p)
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("1\n00:00:00,000 --> 00:00:01,000\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

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
	t.Setenv("MSERVER_STORAGE_SCAN_INTERVAL", "0")
	t.Setenv("MSERVER_FFPROBE_PATH", ffmpegtest.FakeFFprobe(t, ffmpegtest.Movie, 0))
	ctx := t.Context()

	// Both modules on the same server, as in cmd/server.
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := clients.Clients{
		Storage: storageclient.New(srv.Client(), srv.URL),
		Library: client.New(srv.Client(), srv.URL),
	}
	bus := memory.New()
	lib, err := library.New(ctx, t.TempDir(), bus, bus, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	stor, err := storage.New(ctx, t.TempDir(), bus, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stor.Close()
	for _, p := range []string{"/v1/libraries", "/v1/libraries/", "/v1/movies", "/v1/movies/"} {
		mux.Handle(p, lib.Mux)
	}
	mux.Handle("/", stor.Mux)

	dir := t.TempDir()
	old := time.Now().Add(-time.Hour) // stable: analyzed right away
	for _, name := range []string{"Alien (1979)/Alien.mkv", "Alien (1979)/Alien.en.srt"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
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
		page, err := l.ListMovies(ctx, router.MovieQueryV1{LibraryID: created.ID, SortBy: "added", Descending: true, Limit: 10})
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
		movie.Path != "Alien (1979)/Alien.mkv" || movie.MediaInfo.DurationMs != 7_020_000 ||
		len(movie.MediaInfo.Streams) != 2 || movie.MediaInfo.Streams[0].Width != 1920 {
		t.Errorf("movie = %+v (media %+v)", movie, movie.MediaInfo)
	}
	if got, err := l.GetMovie(ctx, movie.ID); err != nil || got.ID != movie.ID || got.MediaInfo == nil {
		t.Errorf("GetMovie = %+v, %v", got, err)
	}
	if page, err := l.ListMovies(ctx, router.MovieQueryV1{IDs: []uuid.UUID{movie.ID, uuid.New()}}); err != nil || page.Total != 1 {
		t.Errorf("ListMovies by IDs = %+v, %v", page, err)
	}
	if page, err := l.ListMovies(ctx, router.MovieQueryV1{Years: []int{1979}, AudioLanguages: []string{"ENG"}}); err != nil || page.Total != 1 {
		t.Errorf("ListMovies by year and audio language = %+v, %v", page, err)
	}
	if page, err := l.ListMovies(ctx, router.MovieQueryV1{Years: []int{1980}}); err != nil || page.Total != 0 {
		t.Errorf("ListMovies by other year = %+v, %v", page, err)
	}
	if page, err := l.ListMovies(ctx, router.MovieQueryV1{SubtitleLanguages: []string{"eng"}}); err != nil || page.Total != 0 {
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
	if _, err := l.GetMovie(ctx, uuid.New()); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("unknown movie: got %v, want ErrNotFound", err)
	}
	if _, err := l.ListMovies(ctx, router.MovieQueryV1{SortBy: "rating"}); !errors.Is(err, client.ErrInvalidArgument) {
		t.Errorf("unknown sort: got %v, want ErrInvalidArgument", err)
	}

	if err := l.DeleteLibrary(ctx, created.ID); err != nil {
		t.Fatalf("DeleteLibrary: %v", err)
	}
	if _, err := l.GetMovie(ctx, movie.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("movie of deleted library: got %v, want ErrNotFound", err)
	}
	if err := l.DeleteLibrary(ctx, created.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("deleted twice: got %v, want ErrNotFound", err)
	}
}

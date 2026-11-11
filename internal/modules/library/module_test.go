package library

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/google/uuid"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
	"github.com/lukerops/mserver/internal/pubsub/memory"
)

// fakeStorage knows every location and accepts every resync.
type fakeStorage struct {
	storagev1connect.StorageServiceClient
}

func (fakeStorage) GetLocation(_ context.Context, req *storagev1.GetLocationRequest) (*storagev1.GetLocationResponse, error) {
	return &storagev1.GetLocationResponse{Location: &storagev1.Location{Id: req.GetId()}}, nil
}

func (fakeStorage) ResyncLocation(context.Context, *storagev1.ResyncLocationRequest) (*storagev1.ResyncLocationResponse, error) {
	return &storagev1.ResyncLocationResponse{}, nil
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

// start runs the module on a database in dir, and returns the bus and a
// client of its API.
func start(t *testing.T, dir string, bus *memory.Bus) (*Module, libraryv1connect.LibraryServiceClient) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := connect.NewServer()
	m, err := New(t.Context(), database.Config{DataDir: dir}, server, bus, bus, fakeStorage{}, log)
	if err != nil {
		t.Fatal(err)
	}
	return m, libraryv1connect.NewLibraryServiceClient(connect.NewClient(connectinprocess.New(server)))
}

func publishFile(t *testing.T, bus *memory.Bus, typ, location, file, path string) {
	t.Helper()
	msg, err := pubsub.Encode(pubsub.NewEnvelope(typ, events.SourceStorage, file, events.FileEventV1{
		FileID:     uuid.MustParse(file),
		LocationID: uuid.MustParse(location),
		Path:       path,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
}

func TestModuleCatalogsStorageEvents(t *testing.T) {
	bus := memory.New()
	t.Cleanup(func() { bus.Close() })
	m, c := start(t, t.TempDir(), bus)
	defer m.Close()
	ctx := t.Context()

	loc := newID()
	created, err := c.CreateLibrary(ctx, &libraryv1.CreateLibraryRequest{
		Name: "Movies", MediaType: "movies", LocationIds: []string{loc},
	})
	if err != nil {
		t.Fatal(err)
	}
	lib := created.GetLibrary()

	// The movie is cataloged from the storage event, in the background.
	file := newID()
	publishFile(t, bus, events.FileIndexedV1, loc, file, "Alien (1979)/Alien.mkv")
	var movie *libraryv1.Movie
	deadline := time.Now().Add(5 * time.Second)
	for movie == nil {
		list, err := c.ListMovies(ctx, &libraryv1.ListMoviesRequest{LibraryId: lib.GetId()})
		if err != nil {
			t.Fatal(err)
		}
		if len(list.GetMovies()) == 1 {
			movie = list.GetMovies()[0]
			if list.GetTotal() != 1 {
				t.Errorf("total = %d, want 1", list.GetTotal())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the movie was not cataloged")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if movie.GetTitle() != "Alien" || movie.GetYear() != 1979 || movie.GetFileId() != file || movie.GetMediaInfo() != nil {
		t.Errorf("movie = %v", movie)
	}

	got, err := c.GetMovie(ctx, &libraryv1.GetMovieRequest{LibraryId: lib.GetId(), Id: movie.GetId()})
	if err != nil || got.GetMovie().GetId() != movie.GetId() {
		t.Errorf("GetMovie = %v, %v", got, err)
	}
	if _, err := c.GetMovie(ctx, &libraryv1.GetMovieRequest{LibraryId: newID(), Id: movie.GetId()}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("movie of another library: code = %v, want NotFound (%v)", connect.CodeOf(err), err)
	}
}

func TestModuleAPIErrors(t *testing.T) {
	bus := memory.New()
	t.Cleanup(func() { bus.Close() })
	m, c := start(t, t.TempDir(), bus)
	defer m.Close()
	ctx := t.Context()

	loc := newID()
	req := &libraryv1.CreateLibraryRequest{Name: "Shows", MediaType: "tvshows", LocationIds: []string{loc}}
	created, err := c.CreateLibrary(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateLibrary(ctx, req); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("location of another library: code = %v, want AlreadyExists (%v)", connect.CodeOf(err), err)
	}
	if _, err := c.CreateLibrary(ctx, &libraryv1.CreateLibraryRequest{Name: "x", MediaType: "music", LocationIds: []string{newID()}}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("bad media type: code = %v, want InvalidArgument (%v)", connect.CodeOf(err), err)
	}
	if _, err := c.CreateLibrary(ctx, &libraryv1.CreateLibraryRequest{Name: "x", MediaType: "movies", LocationIds: []string{"nope"}}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("bad location id: code = %v, want InvalidArgument (%v)", connect.CodeOf(err), err)
	}
	if _, err := c.GetLibrary(ctx, &libraryv1.GetLibraryRequest{Id: newID()}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("unknown library: code = %v, want NotFound (%v)", connect.CodeOf(err), err)
	}
	if _, err := c.GetLibrary(ctx, &libraryv1.GetLibraryRequest{Id: "nope"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("bad library id: code = %v, want InvalidArgument (%v)", connect.CodeOf(err), err)
	}
	for name, req := range map[string]*libraryv1.ListSeriesRequest{
		"negative limit":  {Limit: -1},
		"negative offset": {Offset: -1},
		"bad sort":        {SortBy: "rating"},
		"bad id":          {Ids: []string{"nope"}},
	} {
		if _, err := c.ListSeries(ctx, req); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: code = %v, want InvalidArgument (%v)", name, connect.CodeOf(err), err)
		}
	}
	if _, err := c.ListEpisodes(ctx, &libraryv1.ListEpisodesRequest{SeasonId: newID()}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("episodes of an unknown season: code = %v, want NotFound (%v)", connect.CodeOf(err), err)
	}

	// A huge limit is capped, not rejected.
	if list, err := c.ListSeries(ctx, &libraryv1.ListSeriesRequest{Limit: 1_000_000}); err != nil || list.GetTotal() != 0 {
		t.Errorf("ListSeries with a huge limit = %v, %v", list, err)
	}

	libs, err := c.ListLibraries(ctx, &libraryv1.ListLibrariesRequest{})
	if err != nil || len(libs.GetLibraries()) != 1 || libs.GetLibraries()[0].GetId() != created.GetLibrary().GetId() {
		t.Errorf("ListLibraries = %v, %v", libs, err)
	}
	if _, err := c.ResyncLibrary(ctx, &libraryv1.ResyncLibraryRequest{Id: created.GetLibrary().GetId()}); err != nil {
		t.Errorf("ResyncLibrary: %v", err)
	}
	if _, err := c.DeleteLibrary(ctx, &libraryv1.DeleteLibraryRequest{Id: created.GetLibrary().GetId()}); err != nil {
		t.Errorf("DeleteLibrary: %v", err)
	}
	if _, err := c.DeleteLibrary(ctx, &libraryv1.DeleteLibraryRequest{Id: created.GetLibrary().GetId()}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("deleted twice: code = %v, want NotFound (%v)", connect.CodeOf(err), err)
	}
}

func TestModuleKeepsTheCatalogAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	bus := memory.New()
	t.Cleanup(func() { bus.Close() })

	m, c := start(t, dir, bus)
	created, err := c.CreateLibrary(t.Context(), &libraryv1.CreateLibraryRequest{
		Name: "Movies", MediaType: "movies", LocationIds: []string{newID()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	m, c = start(t, dir, bus)
	defer m.Close()
	if got, err := c.GetLibrary(t.Context(), &libraryv1.GetLibraryRequest{Id: created.GetLibrary().GetId()}); err != nil || got.GetLibrary().GetName() != "Movies" {
		t.Errorf("GetLibrary after restart = %v, %v", got, err)
	}
}

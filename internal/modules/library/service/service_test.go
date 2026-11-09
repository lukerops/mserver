package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	database "github.com/lukerops/mserver/internal/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/library/events"
	"github.com/lukerops/mserver/internal/modules/library/store/sqlite"
	storageclient "github.com/lukerops/mserver/internal/modules/storage/client"
	storageevents "github.com/lukerops/mserver/internal/modules/storage/events"
	storagerouter "github.com/lukerops/mserver/internal/modules/storage/router"
)

// fakeStorage serves the storage API used by the library: it knows a set
// of locations and records the resyncs. The first failResyncs resyncs
// fail.
type fakeStorage struct {
	mu          sync.Mutex
	locations   map[uuid.UUID]bool
	resynced    []uuid.UUID
	failResyncs int
	attempts    int
}

func newFakeStorage(locations ...uuid.UUID) *fakeStorage {
	s := &fakeStorage{locations: make(map[uuid.UUID]bool)}
	for _, loc := range locations {
		s.locations[loc] = true
	}
	return s
}

func (s *fakeStorage) addLocation(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locations[id] = true
}

// client serves s and returns a storage client for it.
func (s *fakeStorage) client(t *testing.T) *storageclient.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/locations/{id}", s.getLocation)
	mux.HandleFunc("POST /v1/locations/{id}/resync", s.resyncLocation)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return storageclient.New(srv.Client(), srv.URL)
}

func (s *fakeStorage) getLocation(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || !s.locations[id] {
		writeJSON(w, http.StatusNotFound, storagerouter.ErrorV1{Error: "location not found"})
		return
	}
	writeJSON(w, http.StatusOK, storagerouter.LocationV1{ID: id, Driver: "local"})
}

func (s *fakeStorage) resyncLocation(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.attempts <= s.failResyncs {
		writeJSON(w, http.StatusServiceUnavailable, storagerouter.ErrorV1{Error: "unavailable"})
		return
	}
	s.resynced = append(s.resynced, uuid.MustParse(r.PathValue("id")))
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newTestService(t *testing.T, pub queue.Publisher, storage *fakeStorage) Service {
	t.Helper()
	ctx := t.Context()
	log := slog.Default()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ob, err := outbox.New(ctx, db, pub, log)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	ob.Start(ctx)
	t.Cleanup(ob.Stop)
	svc := New(st, ob, storage.client(t), log)
	svc.(*service).resyncBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	return svc
}

func createLibrary(t *testing.T, s Service, locations ...uuid.UUID) Library {
	t.Helper()
	lib, err := s.CreateLibrary(t.Context(), CreateLibraryRequest{
		Name:        "Movies",
		MediaType:   MediaTypeMovies,
		LocationIDs: locations,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// storageEvent delivers a storage event to the service.
func storageEvent(t *testing.T, s Service, topic string, data any) {
	t.Helper()
	payload, err := json.Marshal(queue.NewEnvelope(topic, "storage", "", data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleStorageEvent(t.Context(), queue.Message{Topic: topic, Payload: payload}); err != nil {
		t.Fatal(err)
	}
}

func fileEvent(t *testing.T, s Service, topic string, location, file uuid.UUID, path string) {
	t.Helper()
	storageEvent(t, s, topic, storageevents.FileEventV1{FileID: file, LocationID: location, Path: path})
}

func analyzedEvent(t *testing.T, s Service, location, file uuid.UUID, info MediaInfo) {
	t.Helper()
	result, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	storageEvent(t, s, storageevents.FileAnalyzedV1, storageevents.FileAnalyzedEventV1{
		FileID: file, LocationID: location, Analyzer: "ffprobe", Version: 1, Result: result,
	})
}

// libraryEvents collects the library events published on bus.
type libraryEvents chan queue.Message

func subscribeLibrary(bus *memory.Bus) libraryEvents {
	got := make(libraryEvents, 100)
	for _, topic := range []string{events.MovieAddedV1, events.MovieUpdatedV1, events.MovieRemovedV1} {
		bus.Subscribe(topic, func(_ context.Context, msg queue.Message) error {
			got <- msg
			return nil
		})
	}
	return got
}

func (c libraryEvents) next(t *testing.T, topic string) events.MovieEventV1 {
	t.Helper()
	select {
	case msg := <-c:
		var e queue.Envelope[events.MovieEventV1]
		if err := json.Unmarshal(msg.Payload, &e); err != nil {
			t.Fatal(err)
		}
		if msg.Topic != topic || e.Source != events.Source || e.Subject != e.Data.MovieID.String() {
			t.Fatalf("event %s = %+v, want %s", msg.Topic, e, topic)
		}
		return e.Data
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s event", topic)
		return events.MovieEventV1{}
	}
}

func (c libraryEvents) none(t *testing.T) {
	t.Helper()
	select {
	case msg := <-c:
		t.Fatalf("unexpected event %s: %s", msg.Topic, msg.Payload)
	case <-time.After(1500 * time.Millisecond): // longer than the outbox poll
	}
}

func listMovies(t *testing.T, s Service, q MovieQuery) MoviePage {
	t.Helper()
	page, err := s.ListMovies(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestCreateLibrary(t *testing.T) {
	a, b := newID(), newID()
	storage := newFakeStorage(a, b)
	s := newTestService(t, memory.New(), storage)

	lib := createLibrary(t, s, a, b)
	if lib.Name != "Movies" || lib.MediaType != MediaTypeMovies || len(lib.LocationIDs) != 2 {
		t.Fatalf("library = %+v", lib)
	}
	if len(storage.resynced) != 2 || storage.resynced[0] != a || storage.resynced[1] != b {
		t.Errorf("resynced = %v, want both locations", storage.resynced)
	}
	got, err := s.GetLibrary(t.Context(), lib.ID)
	if err != nil || got.ID != lib.ID || len(got.LocationIDs) != 2 || got.LocationIDs[0] != a {
		t.Errorf("GetLibrary = %+v, %v", got, err)
	}
	if libs, err := s.ListLibraries(t.Context()); err != nil || len(libs) != 1 {
		t.Errorf("ListLibraries = %+v, %v", libs, err)
	}

	for name, req := range map[string]CreateLibraryRequest{
		"no name":          {Name: " ", MediaType: MediaTypeMovies, LocationIDs: []uuid.UUID{newID()}},
		"bad media type":   {Name: "Music", MediaType: "music", LocationIDs: []uuid.UUID{newID()}},
		"no location":      {Name: "Movies", MediaType: MediaTypeMovies},
		"repeated":         {Name: "Movies", MediaType: MediaTypeMovies, LocationIDs: []uuid.UUID{a, a}},
		"unknown location": {Name: "Movies", MediaType: MediaTypeMovies, LocationIDs: []uuid.UUID{newID()}},
	} {
		if _, err := s.CreateLibrary(t.Context(), req); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: got %v, want ErrInvalidArgument", name, err)
		}
	}

	c := newID()
	storage.addLocation(c)
	_, err = s.CreateLibrary(t.Context(), CreateLibraryRequest{
		Name: "More", MediaType: MediaTypeMovies, LocationIDs: []uuid.UUID{c, b},
	})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("location of another library: got %v, want ErrAlreadyExists", err)
	}
	if libs, _ := s.ListLibraries(t.Context()); len(libs) != 1 {
		t.Errorf("failed creation left %d libraries", len(libs))
	}

	if _, err := s.GetLibrary(t.Context(), newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown library: got %v, want ErrNotFound", err)
	}
}

func TestCreateLibraryRetriesResync(t *testing.T) {
	loc := newID()

	// Two failures: the second retry succeeds.
	storage := newFakeStorage(loc)
	storage.failResyncs = 2
	createLibrary(t, newTestService(t, memory.New(), storage), loc)
	if storage.attempts != 3 || len(storage.resynced) != 1 {
		t.Errorf("attempts = %d, resynced = %v, want 3 attempts and a resync", storage.attempts, storage.resynced)
	}

	// Every attempt fails: the library is created anyway.
	storage = newFakeStorage(loc)
	storage.failResyncs = 100
	s := newTestService(t, memory.New(), storage)
	lib := createLibrary(t, s, loc)
	if storage.attempts != 3 || len(storage.resynced) != 0 {
		t.Errorf("attempts = %d, resynced = %v, want 3 failed attempts", storage.attempts, storage.resynced)
	}
	if _, err := s.GetLibrary(t.Context(), lib.ID); err != nil {
		t.Errorf("library after failed resync: %v", err)
	}
}

func TestDeleteLibrary(t *testing.T) {
	bus := memory.New()
	evs := subscribeLibrary(bus)
	loc, other := newID(), newID()
	s := newTestService(t, bus, newFakeStorage(loc, other))
	lib := createLibrary(t, s, loc)
	kept := createLibrary(t, s, other)
	file := newID()
	fileEvent(t, s, storageevents.FileIndexedV1, loc, file, "Alien (1979).mkv")
	fileEvent(t, s, storageevents.FileIndexedV1, other, newID(), "Heat (1995).mkv")
	evs.next(t, events.MovieAddedV1)
	evs.next(t, events.MovieAddedV1)

	if err := s.DeleteLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-evs:
		var e queue.Envelope[events.MovieRemovedEventV1]
		if err := json.Unmarshal(msg.Payload, &e); err != nil || msg.Topic != events.MovieRemovedV1 ||
			e.Data != (events.MovieRemovedEventV1{MovieID: movieID(file), LibraryID: lib.ID, FileID: file}) {
			t.Errorf("removed event %s = %+v (%v)", msg.Topic, e, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no removed event")
	}
	if _, err := s.GetLibrary(t.Context(), lib.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted library: got %v, want ErrNotFound", err)
	}
	if page := listMovies(t, s, MovieQuery{}); page.Total != 1 || page.Movies[0].LibraryID != kept.ID {
		t.Errorf("movies after delete = %+v", page)
	}

	// The files of the location are no longer cataloged...
	fileEvent(t, s, storageevents.FileIndexedV1, loc, newID(), "Aliens (1986).mkv")
	evs.none(t)
	// ...until the location joins another library.
	createLibrary(t, s, loc)

	if err := s.DeleteLibrary(t.Context(), lib.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted twice: got %v, want ErrNotFound", err)
	}
}

func TestCatalogFollowsStorage(t *testing.T) {
	bus := memory.New()
	evs := subscribeLibrary(bus)
	loc, other := newID(), newID()
	s := newTestService(t, bus, newFakeStorage(loc, other))
	lib := createLibrary(t, s, loc)
	file := newID()

	// Only the videos of the locations of a library are movies.
	fileEvent(t, s, storageevents.FileIndexedV1, loc, file, "Alien (1979)/Alien.mkv")
	fileEvent(t, s, storageevents.FileIndexedV1, loc, newID(), "Alien (1979)/Alien.en.srt")
	fileEvent(t, s, storageevents.FileIndexedV1, other, newID(), "Heat (1995).mkv")
	added := evs.next(t, events.MovieAddedV1)
	want := events.MovieEventV1{
		MovieID: movieID(file), LibraryID: lib.ID, FileID: file, LocationID: loc,
		Path: "Alien (1979)/Alien.mkv", Title: "Alien", Year: 1979,
	}
	if added != want {
		t.Fatalf("added = %+v, want %+v", added, want)
	}
	page := listMovies(t, s, MovieQuery{})
	if page.Total != 1 || len(page.Movies) != 1 || page.Movies[0].ID != movieID(file) {
		t.Fatalf("movies = %+v", page)
	}

	// Duplicates change nothing.
	fileEvent(t, s, storageevents.FileIndexedV1, loc, file, "Alien (1979)/Alien.mkv")
	evs.none(t)

	info := MediaInfo{
		Container: "matroska,webm", DurationMs: 7_020_000, BitRate: 8_000_000, Size: 7_000_000_000,
		Streams: []Stream{
			{Index: 0, Type: "video", Codec: "h264", Width: 1920, Height: 1080, FrameRate: 23.976},
			{Index: 1, Type: "audio", Codec: "aac", Language: "eng", Channels: 6, Default: true},
		},
	}
	analyzedEvent(t, s, loc, file, info)
	if updated := evs.next(t, events.MovieUpdatedV1); updated.DurationMs != 7_020_000 {
		t.Errorf("updated after analysis = %+v", updated)
	}
	m, err := s.GetMovie(t.Context(), lib.ID, movieID(file))
	if err != nil || m.MediaInfo == nil || len(m.MediaInfo.Streams) != 2 || m.MediaInfo.Streams[0].Width != 1920 {
		t.Fatalf("movie after analysis = %+v, %v", m, err)
	}
	if _, err := s.GetMovie(t.Context(), newID(), movieID(file)); !errors.Is(err, ErrNotFound) {
		t.Errorf("movie of another library: got %v, want ErrNotFound", err)
	}
	if _, err := s.ListMovies(t.Context(), MovieQuery{LibraryID: newID()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("movies of an unknown library: got %v, want ErrNotFound", err)
	}
	analyzedEvent(t, s, loc, file, info)                                                // duplicate
	analyzedEvent(t, s, loc, newID(), info)                                             // not a movie
	storageEvent(t, s, storageevents.FileAnalyzedV1, storageevents.FileAnalyzedEventV1{ // other analyzer
		FileID: file, LocationID: loc, Analyzer: "sha256", Version: 1, Result: json.RawMessage(`"x"`),
	})
	evs.none(t)

	// A move renames the movie and keeps the analysis.
	fileEvent(t, s, storageevents.FileMovedV1, loc, file, "Aliens (1986).mkv")
	moved := evs.next(t, events.MovieUpdatedV1)
	if moved.Title != "Aliens" || moved.Year != 1986 || moved.MovieID != movieID(file) || moved.DurationMs == 0 {
		t.Errorf("updated after move = %+v", moved)
	}

	// A change drops the analysis of the old content.
	fileEvent(t, s, storageevents.FileChangedV1, loc, file, "Aliens (1986).mkv")
	evs.next(t, events.MovieUpdatedV1)
	if m, _ := s.GetMovie(t.Context(), uuid.Nil, movieID(file)); m.MediaInfo != nil || !m.CreatedAt.Equal(page.Movies[0].CreatedAt) {
		t.Errorf("movie after change = %+v", m)
	}

	fileEvent(t, s, storageevents.FileRemovedV1, loc, file, "Aliens (1986).mkv")
	select {
	case msg := <-evs:
		var e queue.Envelope[events.MovieRemovedEventV1]
		if err := json.Unmarshal(msg.Payload, &e); err != nil || msg.Topic != events.MovieRemovedV1 ||
			e.Data != (events.MovieRemovedEventV1{MovieID: movieID(file), LibraryID: lib.ID, FileID: file}) {
			t.Errorf("removed event %s = %+v (%v)", msg.Topic, e, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no removed event")
	}
	if _, err := s.GetMovie(t.Context(), uuid.Nil, movieID(file)); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed movie: got %v, want ErrNotFound", err)
	}
	fileEvent(t, s, storageevents.FileRemovedV1, loc, file, "Aliens (1986).mkv") // duplicate
	evs.none(t)
}

func TestInvalidEventsAreDropped(t *testing.T) {
	s := newTestService(t, memory.New(), newFakeStorage())
	if err := s.HandleStorageEvent(t.Context(), queue.Message{Topic: storageevents.FileIndexedV1, Payload: []byte("{")}); err != nil {
		t.Errorf("invalid event: got %v, want it dropped", err)
	}
}

func TestListMovies(t *testing.T) {
	loc, other := newID(), newID()
	s := newTestService(t, memory.New(), newFakeStorage(loc, other))
	lib := createLibrary(t, s, loc)
	createLibrary(t, s, other)

	files := map[string]uuid.UUID{}
	for _, p := range []string{"The Matrix (1999).mkv", "Alien (1979).mkv", "Heat (1995).mkv"} {
		files[p] = newID()
		fileEvent(t, s, storageevents.FileIndexedV1, loc, files[p], p)
		time.Sleep(time.Millisecond) // distinct creation times
	}
	fileEvent(t, s, storageevents.FileIndexedV1, other, newID(), "Up (2009).mkv")
	analyzedEvent(t, s, loc, files["Alien (1979).mkv"], MediaInfo{Container: "matroska", DurationMs: 1, Streams: []Stream{
		{Index: 0, Type: "audio", Codec: "aac", Language: "eng"},
		{Index: 1, Type: "subtitle", Codec: "subrip", Language: "por"},
	}})
	analyzedEvent(t, s, loc, files["Heat (1995).mkv"], MediaInfo{Container: "matroska", DurationMs: 1, Streams: []Stream{
		{Index: 0, Type: "audio", Codec: "aac", Language: "POR"},
	}})

	titles := func(page MoviePage) []string {
		var out []string
		for _, m := range page.Movies {
			out = append(out, m.Title)
		}
		return out
	}
	for _, tt := range []struct {
		q     MovieQuery
		want  []string
		total int
	}{
		{MovieQuery{LibraryID: lib.ID}, []string{"Alien", "Heat", "The Matrix"}, 3},
		{MovieQuery{LibraryID: lib.ID, Descending: true}, []string{"The Matrix", "Heat", "Alien"}, 3},
		{MovieQuery{LibraryID: lib.ID, SortBy: SortByAdded, Descending: true}, []string{"Heat", "Alien", "The Matrix"}, 3},
		{MovieQuery{LibraryID: lib.ID, SortBy: SortByYear}, []string{"Alien", "Heat", "The Matrix"}, 3},
		{MovieQuery{LibraryID: lib.ID, Offset: 1, Limit: 1}, []string{"Heat"}, 3},
		{MovieQuery{}, []string{"Alien", "Heat", "The Matrix", "Up"}, 4},
		{MovieQuery{IDs: []uuid.UUID{movieID(files["Heat (1995).mkv"])}}, []string{"Heat"}, 1},
		{MovieQuery{Years: []int{1979, 2009}}, []string{"Alien", "Up"}, 2},
		{MovieQuery{LibraryID: lib.ID, Years: []int{1979, 2009}}, []string{"Alien"}, 1},
		{MovieQuery{AudioLanguages: []string{"por"}}, []string{"Heat"}, 1},
		{MovieQuery{AudioLanguages: []string{"ENG", "por"}}, []string{"Alien", "Heat"}, 2},
		{MovieQuery{SubtitleLanguages: []string{"por"}}, []string{"Alien"}, 1},
		{MovieQuery{AudioLanguages: []string{"por"}, SubtitleLanguages: []string{"por"}}, nil, 0},
	} {
		page := listMovies(t, s, tt.q)
		if got := titles(page); fmt.Sprint(got) != fmt.Sprint(tt.want) || page.Total != tt.total {
			t.Errorf("ListMovies(%+v) = %v (total %d), want %v (total %d)", tt.q, got, page.Total, tt.want, tt.total)
		}
	}

	if _, err := s.ListMovies(t.Context(), MovieQuery{SortBy: "rating"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("unknown sort: got %v, want ErrInvalidArgument", err)
	}
}

func TestResyncLibrary(t *testing.T) {
	bus := memory.New()
	evs := subscribeLibrary(bus)
	loc := newID()
	s := newTestService(t, bus, newFakeStorage(loc))
	lib := createLibrary(t, s, loc)
	fileEvent(t, s, storageevents.FileIndexedV1, loc, newID(), "Alien (1979).mkv")
	evs.next(t, events.MovieAddedV1)

	if err := s.ResyncLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	if e := evs.next(t, events.MovieAddedV1); e.Title != "Alien" {
		t.Errorf("resynced = %+v", e)
	}
	if err := s.ResyncLibrary(t.Context(), newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown library: got %v, want ErrNotFound", err)
	}
}

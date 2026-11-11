package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
)

func TestCreateLibrary(t *testing.T) {
	a, b := newID(), newID()
	storage := newFakeStorage(a, b)
	s := newTestService(t, &recorder{}, storage)

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

	for name, req := range map[string]*CreateLibraryRequest{
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
	_, err = s.CreateLibrary(t.Context(), &CreateLibraryRequest{
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
	createLibrary(t, newTestService(t, &recorder{}, storage), loc)
	if storage.attempts != 3 || len(storage.resynced) != 1 {
		t.Errorf("attempts = %d, resynced = %v, want 3 attempts and a resync", storage.attempts, storage.resynced)
	}

	// Every attempt fails: the library is created anyway.
	storage = newFakeStorage(loc)
	storage.failResyncs = 100
	s := newTestService(t, &recorder{}, storage)
	lib := createLibrary(t, s, loc)
	if storage.attempts != 3 || len(storage.resynced) != 0 {
		t.Errorf("attempts = %d, resynced = %v, want 3 failed attempts", storage.attempts, storage.resynced)
	}
	if _, err := s.GetLibrary(t.Context(), lib.ID); err != nil {
		t.Errorf("library after failed resync: %v", err)
	}
}

func TestDeleteLibrary(t *testing.T) {
	rec := &recorder{}
	loc, other := newID(), newID()
	s := newTestService(t, rec, newFakeStorage(loc, other))
	lib := createLibrary(t, s, loc)
	kept := createLibrary(t, s, other)
	file := newID()
	fileEvent(t, s, events.FileIndexedV1, loc, file, "Alien (1979).mkv")
	fileEvent(t, s, events.FileIndexedV1, other, newID(), "Heat (1995).mkv")
	nextMovie(t, rec, events.MovieAddedV1)
	nextMovie(t, rec, events.MovieAddedV1)

	if err := s.DeleteLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	removed := next(t, rec, events.MovieRemovedV1, func(e events.MovieRemovedEventV1) uuid.UUID { return e.MovieID })
	if removed != (events.MovieRemovedEventV1{MovieID: movieID(file), LibraryID: lib.ID, FileID: file}) {
		t.Errorf("removed event = %+v", removed)
	}
	if _, err := s.GetLibrary(t.Context(), lib.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted library: got %v, want ErrNotFound", err)
	}
	if page := listMovies(t, s, &MovieQuery{}); page.Total != 1 || page.Movies[0].LibraryID != kept.ID {
		t.Errorf("movies after delete = %+v", page)
	}

	// The files of the location are no longer cataloged...
	fileEvent(t, s, events.FileIndexedV1, loc, newID(), "Aliens (1986).mkv")
	rec.none(t)
	// ...until the location joins another library.
	createLibrary(t, s, loc)

	if err := s.DeleteLibrary(t.Context(), lib.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted twice: got %v, want ErrNotFound", err)
	}
}

func TestCatalogFollowsStorage(t *testing.T) {
	rec := &recorder{}
	loc, other := newID(), newID()
	s := newTestService(t, rec, newFakeStorage(loc, other))
	lib := createLibrary(t, s, loc)
	file := newID()

	// Only the videos of the locations of a library are movies.
	fileEvent(t, s, events.FileIndexedV1, loc, file, "Alien (1979)/Alien.mkv")
	fileEvent(t, s, events.FileIndexedV1, loc, newID(), "Alien (1979)/Alien.en.srt")
	fileEvent(t, s, events.FileIndexedV1, other, newID(), "Heat (1995).mkv")
	added := nextMovie(t, rec, events.MovieAddedV1)
	want := events.MovieEventV1{
		MovieID: movieID(file), LibraryID: lib.ID, FileID: file, LocationID: loc,
		Path: "Alien (1979)/Alien.mkv", Title: "Alien", Year: 1979,
	}
	if added != want {
		t.Fatalf("added = %+v, want %+v", added, want)
	}
	rec.none(t)
	page := listMovies(t, s, &MovieQuery{})
	if page.Total != 1 || len(page.Movies) != 1 || page.Movies[0].ID != movieID(file) {
		t.Fatalf("movies = %+v", page)
	}

	// Duplicates change nothing.
	fileEvent(t, s, events.FileIndexedV1, loc, file, "Alien (1979)/Alien.mkv")
	rec.none(t)

	info := &MediaInfo{
		Container: "matroska,webm", DurationMs: 7_020_000, BitRate: 8_000_000, Size: 7_000_000_000,
		Streams: []Stream{
			{Index: 0, Type: "video", Codec: "h264", Width: 1920, Height: 1080, FrameRate: 23.976},
			{Index: 1, Type: "audio", Codec: "aac", Language: "eng", Channels: 6, Default: true},
		},
	}
	analyzedEvent(t, s, loc, file, info)
	if updated := nextMovie(t, rec, events.MovieUpdatedV1); updated.DurationMs != 7_020_000 {
		t.Errorf("updated after analysis = %+v", updated)
	}
	m, err := s.GetMovie(t.Context(), lib.ID, movieID(file))
	if err != nil || m.MediaInfo == nil || len(m.MediaInfo.Streams) != 2 || m.MediaInfo.Streams[0].Width != 1920 {
		t.Fatalf("movie after analysis = %+v, %v", m, err)
	}
	if _, err := s.GetMovie(t.Context(), newID(), movieID(file)); !errors.Is(err, ErrNotFound) {
		t.Errorf("movie of another library: got %v, want ErrNotFound", err)
	}
	if _, err := s.ListMovies(t.Context(), &MovieQuery{LibraryID: newID()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("movies of an unknown library: got %v, want ErrNotFound", err)
	}
	analyzedEvent(t, s, loc, file, info)                                  // duplicate
	analyzedEvent(t, s, loc, newID(), info)                               // not a movie
	storageEvent(t, s, events.FileAnalyzedV1, events.FileAnalyzedEventV1{ // other analyzer
		FileID: file, LocationID: loc, Analyzer: "sha256", Version: 1, Result: json.RawMessage(`"x"`),
	})
	rec.none(t)

	// A move renames the movie and keeps the analysis.
	fileEvent(t, s, events.FileMovedV1, loc, file, "Aliens (1986).mkv")
	moved := nextMovie(t, rec, events.MovieUpdatedV1)
	if moved.Title != "Aliens" || moved.Year != 1986 || moved.MovieID != movieID(file) || moved.DurationMs == 0 {
		t.Errorf("updated after move = %+v", moved)
	}

	// A change drops the analysis of the old content.
	fileEvent(t, s, events.FileChangedV1, loc, file, "Aliens (1986).mkv")
	nextMovie(t, rec, events.MovieUpdatedV1)
	if m, _ := s.GetMovie(t.Context(), uuid.Nil, movieID(file)); m.MediaInfo != nil || !m.CreatedAt.Equal(page.Movies[0].CreatedAt) {
		t.Errorf("movie after change = %+v", m)
	}

	fileEvent(t, s, events.FileRemovedV1, loc, file, "Aliens (1986).mkv")
	removed := next(t, rec, events.MovieRemovedV1, func(e events.MovieRemovedEventV1) uuid.UUID { return e.MovieID })
	if removed != (events.MovieRemovedEventV1{MovieID: movieID(file), LibraryID: lib.ID, FileID: file}) {
		t.Errorf("removed event = %+v", removed)
	}
	if _, err := s.GetMovie(t.Context(), uuid.Nil, movieID(file)); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed movie: got %v, want ErrNotFound", err)
	}
	fileEvent(t, s, events.FileRemovedV1, loc, file, "Aliens (1986).mkv") // duplicate
	rec.none(t)
}

func TestNonVideoFilesAreIgnored(t *testing.T) {
	rec := &recorder{}
	loc := newID()
	s := newTestService(t, rec, newFakeStorage(loc))
	lib := createLibrary(t, s, loc)

	// The media type decides, not the extension.
	fileEvent(t, s, events.FileIndexedV1, loc, newID(), "Alien (1979)/Alien.en.srt")
	fileEvent(t, s, events.FileIndexedV1, loc, newID(), "Alien (1979)/poster.jpg")
	fileEvent(t, s, events.FileIndexedV1, loc, newID(), "notes")
	storageEvent(t, s, events.FileIndexedV1, events.FileEventV1{
		FileID: newID(), LocationID: loc, Path: "Heat (1995).mkv", MimeType: "application/octet-stream",
	})
	rec.none(t)
	if page := listMovies(t, s, &MovieQuery{LibraryID: lib.ID}); page.Total != 0 {
		t.Fatalf("movies = %+v, want none", page)
	}

	// A video that becomes something else leaves the catalog.
	file := newID()
	fileEvent(t, s, events.FileIndexedV1, loc, file, "Alien (1979)/Alien.mkv")
	nextMovie(t, rec, events.MovieAddedV1)
	fileEvent(t, s, events.FileMovedV1, loc, file, "Alien (1979)/Alien.srt")
	nextMovie(t, rec, events.MovieRemovedV1)
	if _, err := s.GetMovie(t.Context(), uuid.Nil, movieID(file)); !errors.Is(err, ErrNotFound) {
		t.Errorf("movie of a non-video: got %v, want ErrNotFound", err)
	}
}

func TestInvalidEventsAreDropped(t *testing.T) {
	s := newTestService(t, &recorder{}, newFakeStorage())
	msg := pubsub.Message{Topic: events.FileIndexedV1, Payload: []byte("{")}
	if err := s.HandleStorageEvent(t.Context(), msg); err != nil {
		t.Errorf("invalid event: got %v, want it dropped", err)
	}
}

func TestListMovies(t *testing.T) {
	loc, other := newID(), newID()
	s := newTestService(t, &recorder{}, newFakeStorage(loc, other))
	lib := createLibrary(t, s, loc)
	createLibrary(t, s, other)

	files := map[string]uuid.UUID{}
	for _, p := range []string{"The Matrix (1999).mkv", "Alien (1979).mkv", "Heat (1995).mkv"} {
		files[p] = newID()
		fileEvent(t, s, events.FileIndexedV1, loc, files[p], p)
		time.Sleep(time.Millisecond) // distinct creation times
	}
	fileEvent(t, s, events.FileIndexedV1, other, newID(), "Up (2009).mkv")
	analyzedEvent(t, s, loc, files["Alien (1979).mkv"], &MediaInfo{Container: "matroska", DurationMs: 1, Streams: []Stream{
		{Index: 0, Type: "audio", Codec: "aac", Language: "eng"},
		{Index: 1, Type: "subtitle", Codec: "subrip", Language: "por"},
	}})
	analyzedEvent(t, s, loc, files["Heat (1995).mkv"], &MediaInfo{Container: "matroska", DurationMs: 1, Streams: []Stream{
		{Index: 0, Type: "audio", Codec: "aac", Language: "POR"},
	}})

	titles := func(page *MoviePage) []string {
		var out []string
		for i := range page.Movies {
			out = append(out, page.Movies[i].Title)
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
		page := listMovies(t, s, &tt.q)
		if got := titles(page); fmt.Sprint(got) != fmt.Sprint(tt.want) || page.Total != tt.total {
			t.Errorf("ListMovies(%+v) = %v (total %d), want %v (total %d)", tt.q, got, page.Total, tt.want, tt.total)
		}
	}

	if _, err := s.ListMovies(t.Context(), &MovieQuery{SortBy: "rating"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("unknown sort: got %v, want ErrInvalidArgument", err)
	}
}

func TestResyncLibrary(t *testing.T) {
	rec := &recorder{}
	loc := newID()
	s := newTestService(t, rec, newFakeStorage(loc))
	lib := createLibrary(t, s, loc)
	fileEvent(t, s, events.FileIndexedV1, loc, newID(), "Alien (1979).mkv")
	nextMovie(t, rec, events.MovieAddedV1)

	if err := s.ResyncLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	if e := nextMovie(t, rec, events.MovieAddedV1); e.Title != "Alien" {
		t.Errorf("resynced = %+v", e)
	}
	if err := s.ResyncLibrary(t.Context(), newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown library: got %v, want ErrNotFound", err)
	}
}

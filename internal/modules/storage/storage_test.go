package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/storage/api"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	m := New(DefaultConfig(t.TempDir()), memory.New(), nil)
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := m.svc
	t.Cleanup(func() { m.Stop(t.Context()) })
	return svc
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func createLocation(t *testing.T, s *Service, dir string) api.Location {
	t.Helper()
	loc, err := s.CreateLocation(t.Context(), api.CreateLocationRequest{
		Driver:  "local",
		Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func scan(t *testing.T, s *Service, id api.LocationID) api.ScanResult {
	t.Helper()
	result, err := s.ScanLocation(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// files returns the FileIDs of a location by path.
func files(t *testing.T, s *Service, id api.LocationID) map[string]api.FileID {
	t.Helper()
	list, err := s.ListFiles(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]api.FileID)
	for _, f := range list {
		out[f.Path] = f.ID
	}
	return out
}

func TestScanLifecycle(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Show/ep1.mkv"), "episode 1")
	writeFile(t, filepath.Join(dir, "Show/ep2.mkv"), "episode 2")
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, s, dir)

	if got := scan(t, s, loc.ID); got != (api.ScanResult{Added: 3}) {
		t.Fatalf("first scan = %+v", got)
	}
	before := files(t, s, loc.ID)

	if got := scan(t, s, loc.ID); got != (api.ScanResult{}) {
		t.Fatalf("second scan = %+v", got)
	}

	// Change, rename and remove.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "movie.mkv"), future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "Show/ep1.mkv"), filepath.Join(dir, "Show/S01E01.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "Show/ep2.mkv")); err != nil {
		t.Fatal(err)
	}

	want := api.ScanResult{Changed: 1, Moved: 1, Removed: 1}
	if got := scan(t, s, loc.ID); got != want {
		t.Fatalf("third scan = %+v, want %+v", got, want)
	}

	after := files(t, s, loc.ID)
	if after["movie.mkv"] != before["movie.mkv"] {
		t.Error("changed file got a new FileID")
	}
	if after["Show/S01E01.mkv"] != before["Show/ep1.mkv"] {
		t.Error("renamed file got a new FileID")
	}
	if _, err := s.GetFile(t.Context(), before["Show/ep2.mkv"]); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("removed file: got %v, want ErrNotFound", err)
	}
}

func TestOverlappingLocationsAreRejected(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Series"), 0o755); err != nil {
		t.Fatal(err)
	}
	createLocation(t, s, filepath.Join(dir, "Series"))

	for _, d := range []string{dir, filepath.Join(dir, "Series")} {
		_, err := s.CreateLocation(t.Context(), api.CreateLocationRequest{
			Driver:  "local",
			Options: map[string]string{"dir": d},
		})
		if !errors.Is(err, api.ErrAlreadyExists) {
			t.Errorf("CreateLocation(%s): got %v, want ErrAlreadyExists", d, err)
		}
	}
}

func TestInaccessibleLocationKeepsIndex(t *testing.T) {
	s := newTestService(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "disk")
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)

	// Simulate an unmounted disk.
	if err := os.Rename(dir, filepath.Join(parent, "gone")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScanLocation(t.Context(), loc.ID); !errors.Is(err, api.ErrUnavailable) {
		t.Fatalf("scan: got %v, want ErrUnavailable", err)
	}
	if got := files(t, s, loc.ID); len(got) != 1 {
		t.Errorf("index changed: %v", got)
	}
}

func TestDeleteLocation(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)
	movie := files(t, s, loc.ID)["movie.mkv"]

	if err := s.DeleteLocation(t.Context(), loc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFile(t.Context(), movie); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("file of deleted location: got %v, want ErrNotFound", err)
	}
	if err := s.DeleteLocation(t.Context(), loc.ID); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("second delete: got %v, want ErrNotFound", err)
	}
	// The directory can be registered again.
	createLocation(t, s, dir)
}

func TestSiblingsAndResolve(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Movie/Movie.mkv"), "movie")
	writeFile(t, filepath.Join(dir, "Movie/Movie.en.srt"), "subtitle")
	writeFile(t, filepath.Join(dir, "Other/Other.mkv"), "other")
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)
	ids := files(t, s, loc.ID)

	siblings, err := s.ListSiblings(t.Context(), ids["Movie/Movie.mkv"])
	if err != nil {
		t.Fatal(err)
	}
	want := []api.Sibling{{FileID: ids["Movie/Movie.en.srt"], Path: "Movie/Movie.en.srt"}}
	if !slices.Equal(siblings, want) {
		t.Errorf("siblings = %+v, want %+v", siblings, want)
	}

	u, err := s.Resolve(t.Context(), ids["Movie/Movie.mkv"])
	if err != nil {
		t.Fatal(err)
	}
	if want := "file://" + filepath.ToSlash(filepath.Join(dir, "Movie/Movie.mkv")); u != want {
		t.Errorf("Resolve = %q, want %q", u, want)
	}
}

func TestModuleScansOnStart(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")

	// Register the location in a first run.
	m := New(cfg, memory.New(), nil)
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	loc := createLocation(t, m.svc, dir)
	if err := m.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	// The second run scans it in the background.
	m = New(cfg, memory.New(), nil)
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := m.svc
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(t.Context())

	deadline := time.Now().Add(5 * time.Second)
	for len(files(t, svc, loc.ID)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("location was not scanned on start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFileEventsArePublished(t *testing.T) {
	bus := memory.New()
	got := make(chan queue.Envelope[api.FileEvent], 10)
	for _, typ := range []string{api.EventFileIndexed, api.EventFileChanged, api.EventFileMoved, api.EventFileRemoved} {
		bus.Subscribe(typ, func(_ context.Context, msg queue.Message) error {
			var e queue.Envelope[api.FileEvent]
			if err := json.Unmarshal(msg.Payload, &e); err != nil {
				t.Error(err)
			}
			got <- e
			return nil
		})
	}

	m := New(DefaultConfig(t.TempDir()), bus, nil)
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := m.svc
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(t.Context())

	next := func(want, path string) api.FileEvent {
		t.Helper()
		select {
		case e := <-got:
			ok := e.Type == want && e.Source == api.Source && e.SpecVersion == queue.SpecVersion &&
				e.ID != "" && !e.Time.IsZero() && e.Subject == e.Data.FileID.String() &&
				e.Data.Path == path && e.Data.LocationID != (api.LocationID{})
			if !ok {
				t.Fatalf("event = %+v, want %s for %s", e, want, path)
			}
			return e.Data
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event for %s", want, path)
			return api.FileEvent{}
		}
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, svc, dir)
	scan(t, svc, loc.ID)
	indexed := next(api.EventFileIndexed, "movie.mkv")

	if err := os.Rename(filepath.Join(dir, "movie.mkv"), filepath.Join(dir, "film.mkv")); err != nil {
		t.Fatal(err)
	}
	scan(t, svc, loc.ID)
	if moved := next(api.EventFileMoved, "film.mkv"); moved.FileID != indexed.FileID {
		t.Errorf("moved FileID = %s, want %s", moved.FileID, indexed.FileID)
	}

	if err := svc.DeleteLocation(t.Context(), loc.ID); err != nil {
		t.Fatal(err)
	}
	if removed := next(api.EventFileRemoved, "film.mkv"); removed.FileID != indexed.FileID {
		t.Errorf("removed FileID = %s, want %s", removed.FileID, indexed.FileID)
	}
}

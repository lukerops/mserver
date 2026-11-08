package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	database "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/events"
	"github.com/lukerops/mserver/internal/modules/storage/store/sqlite"
)

// newTestService returns a service over a SQLite database in dataDir,
// publishing its events on pub.
func newTestService(t *testing.T, dataDir string, pub queue.Publisher, analyzers ...analyzer.Analyzer) Service {
	t.Helper()
	ctx := t.Context()
	log := slog.Default()
	db, err := database.Open(ctx, filepath.Join(dataDir, "storage.db"))
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
	return New(st, ob, log, analyzers...)
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

func createLocation(t *testing.T, s Service, dir string) Location {
	t.Helper()
	loc, err := s.CreateLocation(t.Context(), CreateLocationRequest{
		Driver:  "local",
		Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func scan(t *testing.T, s Service, id uuid.UUID) ScanResult {
	t.Helper()
	result, err := s.ScanLocation(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// files returns the file IDs of a location by path.
func files(t *testing.T, s Service, id uuid.UUID) map[string]uuid.UUID {
	t.Helper()
	list, err := s.ListFiles(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]uuid.UUID)
	for _, f := range list {
		out[f.Path] = f.ID
	}
	return out
}

func TestScanLifecycle(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Show/ep1.mkv"), "episode 1")
	writeFile(t, filepath.Join(dir, "Show/ep2.mkv"), "episode 2")
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, s, dir)

	if got := scan(t, s, loc.ID); got != (ScanResult{Added: 3}) {
		t.Fatalf("first scan = %+v", got)
	}
	before := files(t, s, loc.ID)

	if got := scan(t, s, loc.ID); got != (ScanResult{}) {
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

	want := ScanResult{Changed: 1, Moved: 1, Removed: 1}
	if got := scan(t, s, loc.ID); got != want {
		t.Fatalf("third scan = %+v, want %+v", got, want)
	}

	after := files(t, s, loc.ID)
	if after["movie.mkv"] != before["movie.mkv"] {
		t.Error("changed file got a new ID")
	}
	if after["Show/S01E01.mkv"] != before["Show/ep1.mkv"] {
		t.Error("renamed file got a new ID")
	}
	if _, err := s.GetFile(t.Context(), before["Show/ep2.mkv"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed file: got %v, want ErrNotFound", err)
	}
}

func TestInvalidLocationsAreRejected(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
	for _, req := range []CreateLocationRequest{
		{Driver: "nfs", Options: map[string]string{"dir": t.TempDir()}},
		{Driver: "local", Options: map[string]string{"dir": "relative"}},
	} {
		if _, err := s.CreateLocation(t.Context(), req); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("CreateLocation(%+v): got %v, want ErrInvalidArgument", req, err)
		}
	}

	missing := filepath.Join(t.TempDir(), "missing")
	_, err := s.CreateLocation(t.Context(), CreateLocationRequest{
		Driver:  "local",
		Options: map[string]string{"dir": missing},
	})
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Errorf("CreateLocation(missing): got %v, want ErrFailedPrecondition", err)
	}
}

func TestOverlappingLocationsAreRejected(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Series"), 0o755); err != nil {
		t.Fatal(err)
	}
	createLocation(t, s, filepath.Join(dir, "Series"))

	for _, d := range []string{dir, filepath.Join(dir, "Series")} {
		_, err := s.CreateLocation(t.Context(), CreateLocationRequest{
			Driver:  "local",
			Options: map[string]string{"dir": d},
		})
		if !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("CreateLocation(%s): got %v, want ErrAlreadyExists", d, err)
		}
	}
}

func TestInaccessibleLocationKeepsIndex(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
	parent := t.TempDir()
	dir := filepath.Join(parent, "disk")
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)

	// Simulate an unmounted disk.
	if err := os.Rename(dir, filepath.Join(parent, "gone")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScanLocation(t.Context(), loc.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("scan: got %v, want ErrUnavailable", err)
	}
	if got := files(t, s, loc.ID); len(got) != 1 {
		t.Errorf("index changed: %v", got)
	}
}

func TestDeleteLocation(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)
	movie := files(t, s, loc.ID)["movie.mkv"]

	if err := s.DeleteLocation(t.Context(), loc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFile(t.Context(), movie); !errors.Is(err, ErrNotFound) {
		t.Errorf("file of deleted location: got %v, want ErrNotFound", err)
	}
	if err := s.DeleteLocation(t.Context(), loc.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: got %v, want ErrNotFound", err)
	}
	// The directory can be registered again.
	createLocation(t, s, dir)
}

func TestSiblingsAndResolve(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
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
	want := []Sibling{{FileID: ids["Movie/Movie.en.srt"], Path: "Movie/Movie.en.srt"}}
	if !slices.Equal(siblings, want) {
		t.Errorf("siblings = %+v, want %+v", siblings, want)
	}

	u, err := s.Resolve(t.Context(), ids["Movie/Movie.mkv"])
	if err != nil {
		t.Fatal(err)
	}
	if want := "/locations/" + loc.ID.String() + "/files/" + ids["Movie/Movie.mkv"].String(); u != want {
		t.Errorf("Resolve = %q, want %q", u, want)
	}
}

func TestOpenFile(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
	dir := filepath.Join(t.TempDir(), "media")
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	writeFile(t, filepath.Join(dir, "gone.mkv"), "gone")
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)
	ids := files(t, s, loc.ID)

	f, content, err := s.OpenFile(t.Context(), loc.ID, ids["movie.mkv"])
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(content)
	content.Close()
	if err != nil || string(b) != "movie" || f.Path != "movie.mkv" {
		t.Fatalf("OpenFile = %+v, %q, %v", f, b, err)
	}

	if _, _, err := s.OpenFile(t.Context(), newID(), ids["movie.mkv"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("other location: got %v, want ErrNotFound", err)
	}
	if _, _, err := s.OpenFile(t.Context(), loc.ID, newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown file: got %v, want ErrNotFound", err)
	}
	if err := os.Remove(filepath.Join(dir, "gone.mkv")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OpenFile(t.Context(), loc.ID, ids["gone.mkv"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed file: got %v, want ErrNotFound", err)
	}
	// An inaccessible location (e.g. an unmounted disk) is temporary.
	if err := os.Rename(dir, dir+".off"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OpenFile(t.Context(), loc.ID, ids["movie.mkv"]); !errors.Is(err, ErrUnavailable) {
		t.Errorf("inaccessible location: got %v, want ErrUnavailable", err)
	}
}

func TestScanAll(t *testing.T) {
	s := newTestService(t, t.TempDir(), memory.New())
	first, second := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(first, "movie.mkv"), "movie")
	writeFile(t, filepath.Join(second, "show.mkv"), "show")
	a := createLocation(t, s, first)
	b := createLocation(t, s, second)

	s.ScanAll(t.Context())

	if len(files(t, s, a.ID)) != 1 || len(files(t, s, b.ID)) != 1 {
		t.Error("ScanAll did not scan every location")
	}
}

func TestFileEventsArePublished(t *testing.T) {
	bus := memory.New()
	got := make(chan queue.Envelope[events.FileEventV1], 10)
	for _, typ := range []string{events.FileIndexedV1, events.FileChangedV1, events.FileMovedV1, events.FileRemovedV1} {
		bus.Subscribe(typ, func(_ context.Context, msg queue.Message) error {
			var e queue.Envelope[events.FileEventV1]
			if err := json.Unmarshal(msg.Payload, &e); err != nil {
				t.Error(err)
			}
			got <- e
			return nil
		})
	}
	s := newTestService(t, t.TempDir(), bus)

	next := func(want, path string) events.FileEventV1 {
		t.Helper()
		select {
		case e := <-got:
			ok := e.Type == want && e.Source == events.Source && e.SpecVersion == queue.SpecVersion &&
				e.ID != "" && !e.Time.IsZero() && e.Subject == e.Data.FileID.String() &&
				e.Data.Path == path && e.Data.LocationID != uuid.Nil
			if !ok {
				t.Fatalf("event = %+v, want %s for %s", e, want, path)
			}
			return e.Data
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event for %s", want, path)
			return events.FileEventV1{}
		}
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)
	indexed := next(events.FileIndexedV1, "movie.mkv")

	if err := os.Rename(filepath.Join(dir, "movie.mkv"), filepath.Join(dir, "film.mkv")); err != nil {
		t.Fatal(err)
	}
	scan(t, s, loc.ID)
	if moved := next(events.FileMovedV1, "film.mkv"); moved.FileID != indexed.FileID {
		t.Errorf("moved file ID = %s, want %s", moved.FileID, indexed.FileID)
	}

	if err := s.DeleteLocation(t.Context(), loc.ID); err != nil {
		t.Fatal(err)
	}
	if removed := next(events.FileRemovedV1, "film.mkv"); removed.FileID != indexed.FileID {
		t.Errorf("removed file ID = %s, want %s", removed.FileID, indexed.FileID)
	}
}

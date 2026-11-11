package store

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

// TestStore runs the same tests on every available database: SQLite in
// memory, and Postgres when MSERVER_TEST_POSTGRES_URL points to a
// disposable database.
func TestStore(t *testing.T) {
	backends := map[string]func(t *testing.T) *Store{
		"sqlite": func(t *testing.T) *Store {
			return openStore(t, database.Config{Driver: database.SQLite, DataDir: ":memory:"})
		},
	}
	if url := os.Getenv("MSERVER_TEST_POSTGRES_URL"); url != "" {
		backends["postgres"] = func(t *testing.T) *Store {
			return openStore(t, database.Config{Driver: database.Postgres, PostgresURL: url})
		}
	}

	tests := map[string]func(t *testing.T, st *Store){
		"locations":      testLocations,
		"overlap":        testOverlap,
		"scan":           testScan,
		"siblings":       testSiblings,
		"list files":     testListFiles,
		"analyses":       testAnalyses,
		"delete cascade": testDeleteCascade,
	}
	for backend, open := range backends {
		for name, test := range tests {
			t.Run(backend+"/"+name, func(t *testing.T) { test(t, open(t)) })
		}
	}
}

func openStore(t *testing.T, cfg database.Config) *Store {
	t.Helper()
	ctx := t.Context()
	db, err := database.Open(ctx, cfg, "storage", Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cfg.Driver == database.Postgres {
			db.ExecContext(ctx, `DROP SCHEMA storage CASCADE`)
		}
		db.Close()
	})
	return New(db)
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)

func mustInsertLocation(t *testing.T, st *Store, root string) *Location {
	t.Helper()
	loc := &Location{
		ID:        newID(),
		Driver:    "local",
		Options:   map[string]string{"dir": root},
		CreatedAt: epoch,
	}
	if err := st.InsertLocation(t.Context(), loc, root); err != nil {
		t.Fatalf("InsertLocation(%s): %v", root, err)
	}
	return loc
}

func scanFiles(t *testing.T, st *Store, loc *Location, files map[uuid.UUID]driver.FileInfo) {
	t.Helper()
	if err := st.ApplyScan(t.Context(), loc.ID, &ScanChanges{Added: files}); err != nil {
		t.Fatal(err)
	}
}

func info(path string, size int64) driver.FileInfo {
	return driver.FileInfo{Path: path, Size: size, ModTime: epoch, Inode: 42}
}

func testLocations(t *testing.T, st *Store) {
	ctx := t.Context()
	a := mustInsertLocation(t, st, "/media/a")
	b := mustInsertLocation(t, st, "/media/b")

	got, err := st.GetLocation(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.Driver != "local" || got.Options["dir"] != "/media/a" || !got.CreatedAt.Equal(epoch) {
		t.Errorf("GetLocation = %+v, want %+v", got, a)
	}

	if _, err := st.GetLocation(ctx, newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLocation(unknown) error = %v, want ErrNotFound", err)
	}

	list, err := st.ListLocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Errorf("ListLocations = %+v, want [a b] by ID", list)
	}

	if err := st.DeleteLocation(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteLocation(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteLocation twice error = %v, want ErrNotFound", err)
	}
}

func testOverlap(t *testing.T, st *Store) {
	mustInsertLocation(t, st, "/media/tv")

	for _, root := range []string{"/media/tv", "/media/tv/anime", "/media"} {
		loc := &Location{ID: newID(), Driver: "local", CreatedAt: epoch}
		err := st.InsertLocation(t.Context(), loc, root)
		if !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("InsertLocation(%q) error = %v, want ErrAlreadyExists", root, err)
		}
	}

	// A sibling with the same prefix is not an overlap.
	mustInsertLocation(t, st, "/media/tv2")
}

func testScan(t *testing.T, st *Store) {
	ctx := t.Context()
	loc := mustInsertLocation(t, st, "/media")
	keep, move, gone := newID(), newID(), newID()
	scanFiles(t, st, loc, map[uuid.UUID]driver.FileInfo{
		keep: info("a/keep.mkv", 10),
		move: info("a/move.mkv", 20),
		gone: info("a/gone.mkv", 30),
	})

	added := newID()
	moved := info("b/moved.mkv", 20)
	moved.Inode = 7
	err := st.ApplyScan(ctx, loc.ID, &ScanChanges{
		Added:   map[uuid.UUID]driver.FileInfo{added: info("b/new.mkv", 40)},
		Updated: map[uuid.UUID]driver.FileInfo{move: moved},
		Removed: []uuid.UUID{gone},
	})
	if err != nil {
		t.Fatal(err)
	}

	files, err := st.ListFiles(ctx, loc.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	want := []string{"a/keep.mkv", "b/moved.mkv", "b/new.mkv"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths = %v, want %v", paths, want)
		}
	}

	f, err := st.GetFile(ctx, move)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != move || f.LocationID != loc.ID || f.Size != 20 || f.Inode != 7 || !f.ModTime.Equal(epoch) {
		t.Errorf("GetFile = %+v", f)
	}
	if _, err := st.GetFile(ctx, gone); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetFile(removed) error = %v, want ErrNotFound", err)
	}
}

func testSiblings(t *testing.T, st *Store) {
	loc := mustInsertLocation(t, st, "/media")
	movie, sub, other := newID(), newID(), newID()
	scanFiles(t, st, loc, map[uuid.UUID]driver.FileInfo{
		movie:   info("m/film.mkv", 1),
		sub:     info("m/film.srt", 1),
		other:   info("m/sub/film.en.srt", 1),
		newID(): info("film.mkv", 1),
	})

	f, err := st.GetFile(t.Context(), movie)
	if err != nil {
		t.Fatal(err)
	}
	siblings, err := st.Siblings(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(siblings) != 1 || siblings[0].FileID != sub || siblings[0].Path != "m/film.srt" {
		t.Errorf("Siblings = %+v, want only m/film.srt", siblings)
	}
}

func testListFiles(t *testing.T, st *Store) {
	loc := mustInsertLocation(t, st, "/media")
	files := map[uuid.UUID]driver.FileInfo{}
	for _, p := range []string{"a", "b", "c", "d"} {
		files[newID()] = info(p, 1)
	}
	scanFiles(t, st, loc, files)

	tests := []struct {
		page Page
		want []string
	}{
		{Page{}, []string{"a", "b", "c", "d"}},
		{Page{Limit: 2}, []string{"a", "b"}},
		{Page{Limit: 2, Offset: 1}, []string{"b", "c"}},
		{Page{Offset: 3}, []string{"d"}},
		{Page{Offset: 9}, nil},
	}
	for _, tt := range tests {
		got, err := st.ListFiles(t.Context(), loc.ID, tt.page)
		if err != nil {
			t.Fatalf("ListFiles(%+v): %v", tt.page, err)
		}
		var paths []string
		for _, f := range got {
			paths = append(paths, f.Path)
		}
		if len(paths) != len(tt.want) {
			t.Errorf("ListFiles(%+v) = %v, want %v", tt.page, paths, tt.want)
			continue
		}
		for i := range paths {
			if paths[i] != tt.want[i] {
				t.Errorf("ListFiles(%+v) = %v, want %v", tt.page, paths, tt.want)
				break
			}
		}
	}
}

func testAnalyses(t *testing.T, st *Store) {
	ctx := t.Context()
	loc := mustInsertLocation(t, st, "/media")
	id := newID()
	scanFiles(t, st, loc, map[uuid.UUID]driver.FileInfo{id: info("a.mkv", 10)})

	stable := epoch.Add(time.Hour)
	pending, err := st.PendingAnalysis(ctx, "ffprobe", 1, stable)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("PendingAnalysis = %+v, want the file", pending)
	}
	// A file modified after the stability window waits.
	if pending, _ := st.PendingAnalysis(ctx, "ffprobe", 1, epoch.Add(-time.Hour)); len(pending) != 0 {
		t.Errorf("PendingAnalysis(unstable) = %+v, want none", pending)
	}

	a := &Analysis{
		FileID: id, Analyzer: "ffprobe", Version: 1, Size: 10, ModTime: epoch,
		Result: []byte(`{"container":"matroska"}`), AnalyzedAt: epoch,
	}
	if err := st.SaveAnalysis(ctx, a); err != nil {
		t.Fatal(err)
	}
	if pending, _ := st.PendingAnalysis(ctx, "ffprobe", 1, stable); len(pending) != 0 {
		t.Errorf("PendingAnalysis after save = %+v, want none", pending)
	}
	// A newer version of the analyzer analyzes the file again.
	if pending, _ := st.PendingAnalysis(ctx, "ffprobe", 2, stable); len(pending) != 1 {
		t.Errorf("PendingAnalysis(v2) = %+v, want the file", pending)
	}

	list, err := st.ListAnalyses(ctx, loc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || string(list[0].Result) != string(a.Result) || list[0].Version != 1 || !list[0].AnalyzedAt.Equal(epoch) {
		t.Errorf("ListAnalyses = %+v", list)
	}

	// Saving again replaces the analysis; a failure has no result and is
	// not listed.
	failed := *a
	failed.Result, failed.Error = nil, "corrupt file"
	if err := st.SaveAnalysis(ctx, &failed); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListAnalyses(ctx, loc.ID); len(list) != 0 {
		t.Errorf("ListAnalyses after failure = %+v, want none", list)
	}

	// A file that changed no longer matches its analysis.
	if err := st.SaveAnalysis(ctx, a); err != nil {
		t.Fatal(err)
	}
	changed := info("a.mkv", 11)
	if err := st.ApplyScan(ctx, loc.ID, &ScanChanges{Updated: map[uuid.UUID]driver.FileInfo{id: changed}}); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListAnalyses(ctx, loc.ID); len(list) != 0 {
		t.Errorf("ListAnalyses after change = %+v, want none", list)
	}
	if pending, _ := st.PendingAnalysis(ctx, "ffprobe", 1, stable); len(pending) != 1 {
		t.Errorf("PendingAnalysis after change = %+v, want the file", pending)
	}
}

func testDeleteCascade(t *testing.T, st *Store) {
	ctx := t.Context()
	loc := mustInsertLocation(t, st, "/media")
	id := newID()
	scanFiles(t, st, loc, map[uuid.UUID]driver.FileInfo{id: info("a.mkv", 10)})
	if err := st.SaveAnalysis(ctx, &Analysis{
		FileID: id, Analyzer: "ffprobe", Version: 1, Size: 10, ModTime: epoch,
		Result: []byte(`{}`), AnalyzedAt: epoch,
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteLocation(ctx, loc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetFile(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetFile after DeleteLocation error = %v, want ErrNotFound", err)
	}
	if list, err := st.ListAnalyses(ctx, loc.ID); err != nil || len(list) != 0 {
		t.Errorf("ListAnalyses after DeleteLocation = %+v, %v, want none", list, err)
	}
}

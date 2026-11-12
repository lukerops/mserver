package store

import (
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
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
		"subtitle files": testSubtitleFiles,
		"items":          testItems,
		"matches":        testMatches,
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
	db, err := database.Open(ctx, cfg, "subtitles", Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cfg.Driver == database.Postgres {
			db.ExecContext(ctx, `DROP SCHEMA subtitles CASCADE`)
		}
		db.Close()
	})
	return New(db)
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func testSubtitleFiles(t *testing.T, st *Store) {
	ctx := t.Context()
	loc := newID()
	a := &SubtitleFile{FileID: newID(), LocationID: loc, Path: "Movie/Movie.en.srt", Dir: "Movie", Format: "srt"}
	b := &SubtitleFile{FileID: newID(), LocationID: loc, Path: "Movie/Movie.pt.srt", Dir: "Movie", Format: "srt"}
	other := &SubtitleFile{FileID: newID(), LocationID: loc, Path: "Other/Other.srt", Dir: "Other", Format: "srt"}
	for _, f := range []*SubtitleFile{b, a, other} {
		if err := st.SaveSubtitleFile(ctx, f); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.GetSubtitleFile(ctx, a.FileID)
	if err != nil || *got != *a {
		t.Fatalf("GetSubtitleFile = %+v, %v; want %+v", got, err, a)
	}

	// Saving again replaces.
	a.Path, a.Dir = "Moved/Movie.en.srt", "Moved"
	if err := st.SaveSubtitleFile(ctx, a); err != nil {
		t.Fatal(err)
	}
	files, err := st.ListSubtitleFiles(ctx, loc, "Movie")
	if err != nil || len(files) != 1 || files[0] != *b {
		t.Fatalf("ListSubtitleFiles(Movie) = %+v, %v; want only %+v", files, err, b)
	}

	if err := st.DeleteSubtitleFile(ctx, a.FileID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSubtitleFile(ctx, a.FileID); err != nil {
		t.Fatalf("deleting a missing file: %v", err)
	}
	if _, err := st.GetSubtitleFile(ctx, a.FileID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSubtitleFile after delete: err = %v, want ErrNotFound", err)
	}
}

func testItems(t *testing.T, st *Store) {
	ctx := t.Context()
	loc := newID()
	a := &Item{ID: newID(), Type: "movie", FileID: newID(), LocationID: loc, Path: "M/M.mkv", Dir: "M"}
	b := &Item{ID: newID(), Type: "episode", FileID: newID(), LocationID: loc, Path: "M/M.2.mkv", Dir: "M"}
	for _, it := range []*Item{a, b} {
		if err := st.SaveItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.GetItem(ctx, b.ID)
	if err != nil || *got != *b {
		t.Fatalf("GetItem = %+v, %v; want %+v", got, err, b)
	}
	items, err := st.ListItems(ctx, loc, "M")
	if err != nil || len(items) != 2 {
		t.Fatalf("ListItems = %+v, %v; want 2 items", items, err)
	}
	if items[0].ID != b.ID && items[0].ID != a.ID {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items, _ := st.ListItems(ctx, newID(), "M"); len(items) != 0 {
		t.Fatalf("ListItems on another location = %+v", items)
	}

	if err := st.DeleteItem(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetItem(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetItem after delete: err = %v, want ErrNotFound", err)
	}
}

func testMatches(t *testing.T, st *Store) {
	ctx := t.Context()
	loc := newID()
	mk := func(dir, lang string) Match {
		return Match{
			ID: newID(), ItemID: newID(), ItemType: "movie", FileID: newID(),
			LocationID: loc, Dir: dir, Format: "srt", Language: lang, Title: "t",
			Forced: true, HearingImpaired: false, Source: "local",
		}
	}
	a, b, c := mk("A", "en"), mk("A", "pt-BR"), mk("B", "")
	if err := st.ApplyMatches(ctx, []Match{a, b, c}, nil); err != nil {
		t.Fatal(err)
	}

	got, err := st.ListDirMatches(ctx, loc, "A")
	if err != nil || len(got) != 2 {
		t.Fatalf("ListDirMatches(A) = %+v, %v; want 2 matches", got, err)
	}
	for _, m := range got {
		want := a
		if m.ID == b.ID {
			want = b
		}
		if m != want {
			t.Errorf("match = %+v, want %+v", m, want)
		}
	}

	// Replace one, remove another, atomically.
	a.Language, a.HearingImpaired = "fr", true
	if err := st.ApplyMatches(ctx, []Match{a}, []uuid.UUID{b.ID}); err != nil {
		t.Fatal(err)
	}
	got, err = st.ListDirMatches(ctx, loc, "A")
	if err != nil || len(got) != 1 || got[0] != a {
		t.Fatalf("ListDirMatches(A) = %+v, %v; want only %+v", got, err, a)
	}

	page, err := st.ListMatches(ctx, 1, 0)
	if err != nil || len(page) != 1 {
		t.Fatalf("ListMatches(1, 0) = %+v, %v", page, err)
	}
	rest, err := st.ListMatches(ctx, 10, 1)
	if err != nil || len(rest) != 1 || rest[0].ID == page[0].ID {
		t.Fatalf("ListMatches(10, 1) = %+v, %v", rest, err)
	}
}

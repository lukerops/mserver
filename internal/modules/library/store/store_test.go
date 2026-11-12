package store

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

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
		"libraries":         testLibraries,
		"movies":            testMovies,
		"list movies":       testListMovies,
		"save episode":      testSaveEpisode,
		"move episode":      testMoveEpisode,
		"delete episode":    testDeleteEpisode,
		"list episodes":     testListEpisodes,
		"list series":       testListSeries,
		"delete library":    testDeleteLibrary,
		"pagination offset": testOffsetWithoutLimit,
		"subtitles":         testExternalSubtitles,
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
	db, err := database.Open(ctx, cfg, "library", Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cfg.Driver == database.Postgres {
			db.ExecContext(ctx, `DROP SCHEMA library CASCADE`)
		}
		db.Close()
	})
	return New(db)
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)

func intp(n int) *int { return &n }

func newLibrary(t *testing.T, st *Store, mediaType string, locations ...uuid.UUID) *Library {
	t.Helper()
	lib := &Library{ID: newID(), Name: "Lib", MediaType: mediaType, LocationIDs: locations, CreatedAt: epoch}
	if err := st.InsertLibrary(t.Context(), lib); err != nil {
		t.Fatal(err)
	}
	return lib
}

func newMovie(lib *Library, title string, year int, created time.Duration) *Movie {
	return &Movie{
		ID: newID(), LibraryID: lib.ID, FileID: newID(), LocationID: lib.LocationIDs[0],
		Path: title + ".mkv", Title: title, SortTitle: strings.ToLower(title), Year: year,
		CreatedAt: epoch.Add(created), UpdatedAt: epoch.Add(created),
	}
}

func audioInfo(langs ...string) *MediaInfo {
	info := &MediaInfo{Container: "matroska", DurationMs: 1000}
	for i, l := range langs {
		info.Streams = append(info.Streams, Stream{Index: i, Type: "audio", Codec: "aac", Language: l})
	}
	return info
}

// newEpisode returns an episode of a series and season to save; the series
// and season are candidates, used only if they do not exist yet.
func newEpisode(lib *Library, series string, year, season int, number *int, name string) (*Episode, *Series, *Season) {
	e := &Episode{
		ID: newID(), LibraryID: lib.ID, FileID: newID(), LocationID: lib.LocationIDs[0],
		Path: series + "/" + name + ".mkv", Title: name, SortKey: name, Number: number,
		CreatedAt: epoch, UpdatedAt: epoch,
	}
	sr := &Series{
		ID: newID(), LibraryID: lib.ID, Title: series, SortTitle: strings.ToLower(series), Key: strings.ToLower(series),
		Year: year, CreatedAt: epoch,
	}
	sn := &Season{ID: newID(), Number: season, CreatedAt: epoch}
	return e, sr, sn
}

func titlesOfMovies(movies []Movie) []string {
	var out []string
	for i := range movies {
		out = append(out, movies[i].Title)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func testLibraries(t *testing.T, st *Store) {
	ctx := t.Context()
	a, b := newID(), newID()
	lib := newLibrary(t, st, MediaTypeMovies, a, b)

	got, err := st.GetLibrary(ctx, lib.ID)
	if err != nil || got.Name != "Lib" || len(got.LocationIDs) != 2 || got.LocationIDs[0] != a || got.LocationIDs[1] != b ||
		!got.CreatedAt.Equal(epoch) {
		t.Fatalf("GetLibrary = %+v, %v", got, err)
	}
	byLoc, err := st.LibraryOfLocation(ctx, b)
	if err != nil || byLoc.ID != lib.ID {
		t.Errorf("LibraryOfLocation = %+v, %v", byLoc, err)
	}
	if _, err := st.LibraryOfLocation(ctx, newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown location: got %v, want ErrNotFound", err)
	}

	// A location belongs to one library, and a failed insert leaves nothing.
	other := &Library{ID: newID(), Name: "Other", MediaType: MediaTypeMovies, LocationIDs: []uuid.UUID{newID(), b}, CreatedAt: epoch}
	if err := st.InsertLibrary(ctx, other); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("shared location: got %v, want ErrAlreadyExists", err)
	}
	if _, err := st.GetLibrary(ctx, other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("failed insert: got %v, want ErrNotFound", err)
	}

	second := &Library{ID: newID(), Name: "Another", MediaType: MediaTypeTVShows, LocationIDs: []uuid.UUID{newID()}, CreatedAt: epoch}
	if err := st.InsertLibrary(ctx, second); err != nil {
		t.Fatal(err)
	}
	libs, err := st.ListLibraries(ctx)
	if err != nil || len(libs) != 2 || libs[0].Name != "Another" || libs[1].Name != "Lib" || len(libs[1].LocationIDs) != 2 {
		t.Errorf("ListLibraries = %+v, %v", libs, err)
	}

	if err := st.DeleteLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteLibrary(ctx, lib.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted twice: got %v, want ErrNotFound", err)
	}
	// Its locations are free again.
	if err := st.InsertLibrary(ctx, other); err != nil {
		t.Errorf("location of a deleted library: %v", err)
	}
}

func testMovies(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeMovies, newID())
	m := newMovie(lib, "Alien", 1979, 0)

	if _, err := st.GetMovie(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown movie: got %v, want ErrNotFound", err)
	}
	if err := st.SaveMovie(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetMovie(ctx, m.ID)
	if err != nil || got.Title != "Alien" || got.Year != 1979 || got.FileID != m.FileID || got.MediaInfo != nil ||
		!got.CreatedAt.Equal(m.CreatedAt) {
		t.Fatalf("GetMovie = %+v, %v", got, err)
	}

	// Saving again replaces the movie.
	m.Title, m.MediaInfo = "Aliens", audioInfo("eng")
	if err := st.SaveMovie(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetMovie(ctx, m.ID)
	if got.Title != "Aliens" || !got.MediaInfo.Equal(m.MediaInfo) {
		t.Errorf("saved again = %+v", got)
	}

	info := &MediaInfo{Container: "mp4", DurationMs: 5, Streams: []Stream{{Index: 0, Type: "video", Codec: "h264", FrameRate: 23.976}}}
	if err := st.UpdateMovieMediaInfo(ctx, m.ID, info, epoch.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetMovie(ctx, m.ID)
	if !got.MediaInfo.Equal(info) || !got.UpdatedAt.Equal(epoch.Add(time.Hour)) {
		t.Errorf("after media info update = %+v", got)
	}
	if err := st.UpdateMovieMediaInfo(ctx, newID(), info, epoch); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of an unknown movie: got %v, want ErrNotFound", err)
	}

	if err := st.DeleteMovie(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetMovie(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted movie: got %v, want ErrNotFound", err)
	}
}

func testListMovies(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeMovies, newID())
	other := newLibrary(t, st, MediaTypeMovies, newID())

	matrix := newMovie(lib, "The Matrix", 1999, 0)
	alien := newMovie(lib, "alien", 1979, time.Second)
	alien.MediaInfo = &MediaInfo{Streams: []Stream{
		{Type: "audio", Language: "eng"}, {Type: "subtitle", Language: "por"},
	}}
	heat := newMovie(lib, "Heat", 1995, 2*time.Second)
	heat.MediaInfo = audioInfo("POR")
	up := newMovie(other, "Up", 2009, 3*time.Second)
	for _, m := range []*Movie{matrix, alien, heat, up} {
		if err := st.SaveMovie(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	for _, tt := range []struct {
		q     MovieQuery
		want  []string
		total int
	}{
		// The title sort uses the sort title, lower case.
		{MovieQuery{LibraryID: lib.ID}, []string{"alien", "Heat", "The Matrix"}, 3},
		{MovieQuery{LibraryID: lib.ID, Descending: true}, []string{"The Matrix", "Heat", "alien"}, 3},
		{MovieQuery{LibraryID: lib.ID, SortBy: SortByAdded, Descending: true}, []string{"Heat", "alien", "The Matrix"}, 3},
		{MovieQuery{LibraryID: lib.ID, SortBy: SortByYear}, []string{"alien", "Heat", "The Matrix"}, 3},
		{MovieQuery{LibraryID: lib.ID, Offset: 1, Limit: 1}, []string{"Heat"}, 3},
		{MovieQuery{}, []string{"alien", "Heat", "The Matrix", "Up"}, 4},
		{MovieQuery{IDs: []uuid.UUID{heat.ID, up.ID}}, []string{"Heat", "Up"}, 2},
		{MovieQuery{Years: []int{1979, 2009}}, []string{"alien", "Up"}, 2},
		{MovieQuery{LibraryID: lib.ID, Years: []int{1979, 2009}}, []string{"alien"}, 1},
		{MovieQuery{AudioLanguages: []string{"por"}}, []string{"Heat"}, 1},
		{MovieQuery{AudioLanguages: []string{"ENG", "por"}}, []string{"alien", "Heat"}, 2},
		{MovieQuery{SubtitleLanguages: []string{"por"}}, []string{"alien"}, 1},
		{MovieQuery{AudioLanguages: []string{"por"}, SubtitleLanguages: []string{"por"}}, nil, 0},
		// LIKE wildcards in a language are literals.
		{MovieQuery{AudioLanguages: []string{"%"}}, nil, 0},
		{MovieQuery{AudioLanguages: []string{"_ng"}}, nil, 0},
	} {
		movies, total, err := st.ListMovies(ctx, &tt.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := titlesOfMovies(movies); !equalStrings(got, tt.want) || total != tt.total {
			t.Errorf("ListMovies(%+v) = %v (total %d), want %v (total %d)", tt.q, got, total, tt.want, tt.total)
		}
	}
}

func testSaveEpisode(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeTVShows, newID())

	// The first episode creates its series and season.
	e1, sr1, sn1 := newEpisode(lib, "Breaking Bad", 2008, 1, intp(1), "Pilot")
	changes, err := st.SaveEpisode(ctx, e1, sr1, sn1)
	if err != nil {
		t.Fatal(err)
	}
	if changes.AddedSeries != sr1 || changes.AddedSeason != sn1 || changes.RemovedSeason != nil || changes.RemovedSeries != nil {
		t.Fatalf("changes = %+v", changes)
	}
	if e1.SeriesID != sr1.ID || e1.SeasonID != sn1.ID || e1.SeriesTitle != "Breaking Bad" || e1.SeasonNumber != 1 ||
		sn1.SeriesID != sr1.ID || sn1.LibraryID != lib.ID {
		t.Errorf("episode after save = %+v, season = %+v", e1, sn1)
	}

	// The second joins them: the candidates are discarded.
	e2, sr2, sn2 := newEpisode(lib, "Breaking Bad", 2008, 1, intp(2), "Cat's in the Bag")
	e2.EndNumber = intp(3)
	changes, err = st.SaveEpisode(ctx, e2, sr2, sn2)
	if err != nil {
		t.Fatal(err)
	}
	if changes.AddedSeries != nil || changes.AddedSeason != nil || e2.SeriesID != sr1.ID || e2.SeasonID != sn1.ID {
		t.Errorf("changes = %+v, episode = %+v", changes, e2)
	}

	// Another year is another series.
	e3, sr3, sn3 := newEpisode(lib, "Breaking Bad", 2024, 1, nil, "Remake")
	if changes, err = st.SaveEpisode(ctx, e3, sr3, sn3); err != nil || changes.AddedSeries == nil || e3.SeriesID != sr3.ID {
		t.Errorf("remake: changes = %+v, %v", changes, err)
	}

	got, err := st.GetEpisode(ctx, e2.ID)
	if err != nil || got.Number == nil || *got.Number != 2 || got.EndNumber == nil || *got.EndNumber != 3 ||
		got.SeriesTitle != "Breaking Bad" || got.SeriesYear != 2008 || got.SeasonNumber != 1 || got.SeasonID != sn1.ID {
		t.Errorf("GetEpisode = %+v, %v", got, err)
	}
	if got, _ := st.GetEpisode(ctx, e3.ID); got.Number != nil || got.EndNumber != nil {
		t.Errorf("episode without number = %+v", got)
	}
	if _, err := st.GetEpisode(ctx, newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown episode: got %v, want ErrNotFound", err)
	}

	sr, err := st.GetSeries(ctx, sr1.ID)
	if err != nil || sr.SeasonCount != 1 || sr.EpisodeCount != 2 {
		t.Errorf("series = %+v, %v", sr, err)
	}
	sn, err := st.GetSeason(ctx, sn1.ID)
	if err != nil || sn.EpisodeCount != 2 || sn.SeriesTitle != "Breaking Bad" || sn.SeriesYear != 2008 || sn.SeriesID != sr1.ID {
		t.Errorf("season = %+v, %v", sn, err)
	}

	// Saving the same episode again changes nothing in the show.
	e1.Title = "Pilot!"
	_, sr1b, sn1b := newEpisode(lib, "Breaking Bad", 2008, 1, intp(1), "x")
	e1.SeriesID, e1.SeasonID = uuid.Nil, uuid.Nil
	if changes, err = st.SaveEpisode(ctx, e1, sr1b, sn1b); err != nil || changes.AddedSeries != nil || changes.AddedSeason != nil ||
		changes.RemovedSeason != nil || e1.SeasonID != sn1.ID {
		t.Errorf("save again: changes = %+v, %v", changes, err)
	}
	if got, _ := st.GetEpisode(ctx, e1.ID); got.Title != "Pilot!" {
		t.Errorf("episode saved again = %+v", got)
	}
}

func testMoveEpisode(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeTVShows, newID())
	e, sr, sn := newEpisode(lib, "Show", 0, 0, nil, "Special")
	if _, err := st.SaveEpisode(ctx, e, sr, sn); err != nil {
		t.Fatal(err)
	}

	// Moving the only episode of the specials to season 1 removes the
	// specials, and keeps the series.
	_, sr1, sn1 := newEpisode(lib, "Show", 0, 1, nil, "x")
	changes, err := st.SaveEpisode(ctx, e, sr1, sn1)
	if err != nil {
		t.Fatal(err)
	}
	if changes.AddedSeries != nil || changes.AddedSeason != sn1 || changes.RemovedSeason == nil ||
		changes.RemovedSeason.ID != sn.ID || changes.RemovedSeason.Number != 0 || changes.RemovedSeries != nil {
		t.Fatalf("changes = %+v", changes)
	}
	if _, err := st.GetSeason(ctx, sn.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("emptied season: got %v, want ErrNotFound", err)
	}

	// Moving it to another series removes the old season and series.
	_, sr2, sn2 := newEpisode(lib, "Other Show", 0, 1, nil, "x")
	e.Path = "Other Show/x.mkv"
	changes, err = st.SaveEpisode(ctx, e, sr2, sn2)
	if err != nil {
		t.Fatal(err)
	}
	if changes.AddedSeries != sr2 || changes.AddedSeason != sn2 || changes.RemovedSeason == nil || changes.RemovedSeries == nil ||
		changes.RemovedSeries.ID != sr.ID || changes.RemovedSeries.Title != "Show" {
		t.Fatalf("changes = %+v", changes)
	}
	if page, total, err := st.ListSeries(ctx, &SeriesQuery{}); err != nil || total != 1 || page[0].Title != "Other Show" {
		t.Errorf("series = %+v (%d), %v", page, total, err)
	}
}

func testDeleteEpisode(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeTVShows, newID())
	e1, sr, sn := newEpisode(lib, "Show", 2000, 1, intp(1), "a")
	if _, err := st.SaveEpisode(ctx, e1, sr, sn); err != nil {
		t.Fatal(err)
	}
	e2, sr2, sn2 := newEpisode(lib, "Show", 2000, 1, intp(2), "b")
	if _, err := st.SaveEpisode(ctx, e2, sr2, sn2); err != nil {
		t.Fatal(err)
	}

	changes, err := st.DeleteEpisode(ctx, e1.ID)
	if err != nil || changes.RemovedSeason != nil || changes.RemovedSeries != nil {
		t.Fatalf("first delete: changes = %+v, %v", changes, err)
	}
	changes, err = st.DeleteEpisode(ctx, e2.ID)
	if err != nil || changes.RemovedSeason == nil || changes.RemovedSeason.ID != sn.ID || changes.RemovedSeason.SeriesID != sr.ID ||
		changes.RemovedSeries == nil || changes.RemovedSeries.ID != sr.ID || changes.RemovedSeries.LibraryID != lib.ID ||
		changes.RemovedSeries.Title != "Show" || changes.RemovedSeries.Year != 2000 {
		t.Fatalf("last delete: changes = %+v, %v", changes, err)
	}
	if _, err := st.GetSeries(ctx, sr.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("emptied series: got %v, want ErrNotFound", err)
	}

	// Deleting a missing episode changes nothing.
	changes, err = st.DeleteEpisode(ctx, e2.ID)
	if err != nil || *changes != (ShowChanges{}) {
		t.Errorf("missing episode: changes = %+v, %v", changes, err)
	}
}

func testListEpisodes(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeTVShows, newID())
	ids := map[string]uuid.UUID{}
	save := func(series string, season int, number *int, name string, info *MediaInfo) {
		e, sr, sn := newEpisode(lib, series, 0, season, number, name)
		e.MediaInfo = info
		if _, err := st.SaveEpisode(ctx, e, sr, sn); err != nil {
			t.Fatal(err)
		}
		ids[name] = e.ID
	}
	save("B Show", 1, intp(2), "b1e2", nil)
	save("B Show", 1, nil, "b1-extra", nil)
	save("B Show", 1, intp(1), "b1e1", audioInfo("eng"))
	save("B Show", 0, nil, "b0", nil)
	save("A Show", 2, intp(1), "a2e1", audioInfo("por"))
	save("A Show", 1, nil, "a1-z10", nil)
	save("A Show", 1, nil, "a1-a", nil)

	names := func(eps []Episode) []string {
		var out []string
		for i := range eps {
			out = append(out, eps[i].Title)
		}
		return out
	}
	for _, tt := range []struct {
		q     EpisodeQuery
		want  []string
		total int
	}{
		// By series title, season, number (episodes without number last,
		// ordered by file name).
		{EpisodeQuery{}, []string{"a1-a", "a1-z10", "a2e1", "b0", "b1e1", "b1e2", "b1-extra"}, 7},
		{EpisodeQuery{Offset: 5}, []string{"b1e2", "b1-extra"}, 7},
		{EpisodeQuery{Limit: 2, Offset: 1}, []string{"a1-z10", "a2e1"}, 7},
		{EpisodeQuery{AudioLanguages: []string{"eng", "por"}}, []string{"a2e1", "b1e1"}, 2},
		{EpisodeQuery{AudioLanguages: []string{"fra"}}, nil, 0},
		{EpisodeQuery{IDs: []uuid.UUID{ids["b0"], ids["b1e1"]}}, []string{"b0", "b1e1"}, 2},
	} {
		eps, total, err := st.ListEpisodes(ctx, &tt.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := names(eps); !equalStrings(got, tt.want) || total != tt.total {
			t.Errorf("ListEpisodes(%+v) = %v (total %d), want %v (total %d)", tt.q, got, total, tt.want, tt.total)
		}
	}

	// Filter by series and by season.
	b2, err := st.GetEpisode(ctx, ids["b1e2"])
	if err != nil {
		t.Fatal(err)
	}
	eps, total, err := st.ListEpisodes(ctx, &EpisodeQuery{SeriesID: b2.SeriesID, SeasonID: b2.SeasonID})
	if got := names(eps); err != nil || total != 3 || !equalStrings(got, []string{"b1e1", "b1e2", "b1-extra"}) {
		t.Errorf("episodes of the season = %v (%d), %v", got, total, err)
	}
	eps, _, _ = st.ListEpisodes(ctx, &EpisodeQuery{LibraryID: lib.ID, SortBy: SortByNumber, Descending: true, Limit: 1})
	if got := names(eps); !equalStrings(got, []string{"b1-extra"}) {
		t.Errorf("descending = %v", got)
	}

	seasons, err := st.ListSeasons(ctx, &SeasonQuery{LibraryID: lib.ID})
	if err != nil || len(seasons) != 4 {
		t.Fatalf("seasons = %+v, %v", seasons, err)
	}
	want := []struct {
		title  string
		number int
	}{{"A Show", 1}, {"A Show", 2}, {"B Show", 0}, {"B Show", 1}}
	for i, w := range want {
		if seasons[i].SeriesTitle != w.title || seasons[i].Number != w.number {
			t.Errorf("season %d = %+v, want %v", i, seasons[i], w)
		}
	}

	if err := st.UpdateEpisodeMediaInfo(ctx, ids["b0"], audioInfo("fra"), epoch.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListEpisodes(ctx, &EpisodeQuery{AudioLanguages: []string{"fra"}}); total != 1 {
		t.Errorf("episodes in french after the update = %d", total)
	}
	if err := st.UpdateEpisodeMediaInfo(ctx, newID(), audioInfo("fra"), epoch); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of an unknown episode: got %v, want ErrNotFound", err)
	}
}

func testListSeries(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeTVShows, newID())
	save := func(series string, year int, info *MediaInfo, created time.Duration) {
		e, sr, sn := newEpisode(lib, series, year, 1, intp(1), "e")
		e.MediaInfo = info
		sr.CreatedAt = epoch.Add(created)
		if _, err := st.SaveEpisode(ctx, e, sr, sn); err != nil {
			t.Fatal(err)
		}
	}
	save("Bravo", 2010, audioInfo("eng"), 0)
	save("alpha", 2020, audioInfo("por"), time.Second)
	save("Charlie", 2010, nil, 2*time.Second)

	titles := func(series []Series) []string {
		var out []string
		for i := range series {
			out = append(out, series[i].Title)
		}
		return out
	}
	for _, tt := range []struct {
		q     SeriesQuery
		want  []string
		total int
	}{
		{SeriesQuery{LibraryID: lib.ID}, []string{"alpha", "Bravo", "Charlie"}, 3},
		{SeriesQuery{SortBy: SortByAdded, Descending: true}, []string{"Charlie", "alpha", "Bravo"}, 3},
		{SeriesQuery{SortBy: SortByYear, Descending: true}, []string{"alpha", "Bravo", "Charlie"}, 3},
		{SeriesQuery{Years: []int{2010}}, []string{"Bravo", "Charlie"}, 2},
		{SeriesQuery{AudioLanguages: []string{"POR"}}, []string{"alpha"}, 1},
		{SeriesQuery{AudioLanguages: []string{"fra"}}, nil, 0},
		{SeriesQuery{Limit: 1, Offset: 1}, []string{"Bravo"}, 3},
	} {
		series, total, err := st.ListSeries(ctx, &tt.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := titles(series); !equalStrings(got, tt.want) || total != tt.total {
			t.Errorf("ListSeries(%+v) = %v (total %d), want %v (total %d)", tt.q, got, total, tt.want, tt.total)
		}
	}
}

func testDeleteLibrary(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeTVShows, newID())
	e, sr, sn := newEpisode(lib, "Show", 0, 1, intp(1), "e")
	if _, err := st.SaveEpisode(ctx, e, sr, sn); err != nil {
		t.Fatal(err)
	}
	m := newMovie(lib, "Movie", 2000, 0)
	if err := st.SaveMovie(ctx, m); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	// The catalog goes with the library.
	if _, err := st.GetEpisode(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("episode: got %v, want ErrNotFound", err)
	}
	if _, err := st.GetSeason(ctx, sn.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("season: got %v, want ErrNotFound", err)
	}
	if _, err := st.GetSeries(ctx, sr.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("series: got %v, want ErrNotFound", err)
	}
	if _, err := st.GetMovie(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("movie: got %v, want ErrNotFound", err)
	}
}

func testOffsetWithoutLimit(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeMovies, newID())
	for i, title := range []string{"a", "b", "c"} {
		if err := st.SaveMovie(ctx, newMovie(lib, title, 2000, time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	movies, total, err := st.ListMovies(ctx, &MovieQuery{Offset: 2})
	if got := titlesOfMovies(movies); err != nil || total != 3 || !equalStrings(got, []string{"c"}) {
		t.Errorf("offset only = %v (%d), %v", got, total, err)
	}
}

func TestMediaInfoEqual(t *testing.T) {
	a := audioInfo("eng")
	for _, tt := range []struct {
		name string
		a, b *MediaInfo
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil and set", nil, a, false},
		{"set and nil", a, nil, false},
		{"same", a, audioInfo("eng"), true},
		{"other language", a, audioInfo("por"), false},
		{"other stream count", a, audioInfo("eng", "por"), false},
		{"other duration", a, &MediaInfo{Container: "matroska", DurationMs: 2, Streams: a.Streams}, false},
	} {
		if got := tt.a.Equal(tt.b); got != tt.want {
			t.Errorf("%s: Equal = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func testExternalSubtitles(t *testing.T, st *Store) {
	ctx := t.Context()
	lib := newLibrary(t, st, MediaTypeTVShows, newID())
	subs := []ExternalSubtitle{
		{ID: newID(), FileID: newID(), Format: "srt", Language: "pt-BR", Source: "local"},
		{ID: newID(), FileID: newID(), Format: "srt", Language: "en", Title: "Commentary", Forced: true, HearingImpaired: true, Source: "local"},
	}

	m := newMovie(lib, "Alien", 1979, 0)
	m.ExternalSubtitles = subs[:1]
	if err := st.SaveMovie(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetMovie(ctx, m.ID)
	if !slices.Equal(got.ExternalSubtitles, subs[:1]) {
		t.Fatalf("saved movie subtitles = %+v, want %+v", got.ExternalSubtitles, subs[:1])
	}
	if err := st.UpdateMovieSubtitles(ctx, m.ID, subs, epoch.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetMovie(ctx, m.ID)
	if !slices.Equal(got.ExternalSubtitles, subs) || !got.UpdatedAt.Equal(epoch.Add(time.Hour)) {
		t.Fatalf("movie after update = %+v", got)
	}
	if err := st.UpdateMovieSubtitles(ctx, m.ID, nil, epoch); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.GetMovie(ctx, m.ID); len(got.ExternalSubtitles) != 0 {
		t.Errorf("movie subtitles after clearing = %+v", got.ExternalSubtitles)
	}
	if err := st.UpdateMovieSubtitles(ctx, newID(), subs, epoch); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of an unknown movie: got %v, want ErrNotFound", err)
	}

	e, sr, sn := newEpisode(lib, "Breaking Bad", 2008, 1, intp(1), "Pilot")
	if _, err := st.SaveEpisode(ctx, e, sr, sn); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateEpisodeSubtitles(ctx, e.ID, subs, epoch.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	gotEp, _ := st.GetEpisode(ctx, e.ID)
	if !slices.Equal(gotEp.ExternalSubtitles, subs) || !gotEp.UpdatedAt.Equal(epoch.Add(time.Hour)) {
		t.Fatalf("episode after update = %+v", gotEp)
	}
	if err := st.UpdateEpisodeSubtitles(ctx, newID(), subs, epoch); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of an unknown episode: got %v, want ErrNotFound", err)
	}
	// The listings read them too.
	list, _, err := st.ListEpisodes(ctx, &EpisodeQuery{})
	if err != nil || len(list) != 1 || !slices.Equal(list[0].ExternalSubtitles, subs) {
		t.Errorf("ListEpisodes = %+v, %v", list, err)
	}
}

func TestSortSubtitles(t *testing.T) {
	a := ExternalSubtitle{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Language: "en"}
	b := ExternalSubtitle{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Language: "en"}
	c := ExternalSubtitle{ID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), Language: "en", Title: "x"}
	d := ExternalSubtitle{ID: uuid.MustParse("00000000-0000-0000-0000-000000000004"), Language: "en", Title: "x", Forced: true}
	e := ExternalSubtitle{ID: uuid.MustParse("00000000-0000-0000-0000-000000000005")}
	f := ExternalSubtitle{ID: uuid.MustParse("00000000-0000-0000-0000-000000000006"), Language: "pt-BR"}
	got := []ExternalSubtitle{f, d, a, e, c, b}
	SortSubtitles(got)
	want := []ExternalSubtitle{e, b, a, c, d, f}
	if !slices.Equal(got, want) {
		t.Errorf("SortSubtitles = %+v, want %+v", got, want)
	}
}

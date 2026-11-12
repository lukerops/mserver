package service

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/subtitles/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// recorder is a publisher that keeps the messages.
type recorder struct {
	mu   sync.Mutex
	msgs []pubsub.Message
}

func (r *recorder) Publish(_ context.Context, msg pubsub.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	return nil
}

// take returns the messages published since the last call.
func (r *recorder) take() []pubsub.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	msgs := r.msgs
	r.msgs = nil
	return msgs
}

type fixture struct {
	t   *testing.T
	svc *Service
	rec *recorder
	loc uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := database.Open(t.Context(), database.Config{DataDir: ":memory:"}, "subtitles", store.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rec := new(recorder)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &fixture{t: t, svc: New(store.New(db), rec, log), rec: rec, loc: newID()}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func (f *fixture) storage(topic string, file uuid.UUID, path, mime string) {
	f.t.Helper()
	msg, err := pubsub.Encode(pubsub.NewEnvelope(topic, events.SourceStorage, file.String(), events.FileEventV1{
		FileID: file, LocationID: f.loc, Path: path, MimeType: mime,
	}))
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.svc.HandleStorageEvent(f.t.Context(), msg); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) subtitle(topic string, file uuid.UUID, path string) {
	f.t.Helper()
	f.storage(topic, file, path, "application/x-subrip")
}

func (f *fixture) library(topic string, msgData any, subject uuid.UUID) {
	f.t.Helper()
	msg, err := pubsub.Encode(pubsub.NewEnvelope(topic, events.SourceLibrary, subject.String(), msgData))
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.svc.HandleLibraryEvent(f.t.Context(), msg); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) movie(topic string, id, file uuid.UUID, path string) {
	f.t.Helper()
	f.library(topic, events.MovieEventV1{MovieID: id, FileID: file, LocationID: f.loc, Path: path}, id)
}

func (f *fixture) episode(topic string, id, file uuid.UUID, path string) {
	f.t.Helper()
	f.library(topic, events.EpisodeEventV1{EpisodeID: id, FileID: file, LocationID: f.loc, Path: path}, id)
}

func (f *fixture) removeMovie(id uuid.UUID) {
	f.t.Helper()
	f.library(events.MovieRemovedV1, events.MovieRemovedEventV1{MovieID: id}, id)
}

// added and removed decode the events published since the last call.
func (f *fixture) published() (added []events.SubtitleEventV1, removed []events.SubtitleRemovedEventV1) {
	f.t.Helper()
	for _, msg := range f.rec.take() {
		switch msg.Topic {
		case events.SubtitleAddedV1:
			e, err := pubsub.Decode[events.SubtitleEventV1](msg)
			if err != nil {
				f.t.Fatal(err)
			}
			added = append(added, e.Data)
		case events.SubtitleRemovedV1:
			e, err := pubsub.Decode[events.SubtitleRemovedEventV1](msg)
			if err != nil {
				f.t.Fatal(err)
			}
			removed = append(removed, e.Data)
		default:
			f.t.Fatalf("unexpected event %s", msg.Topic)
		}
	}
	return added, removed
}

const mkv = "video/x-matroska"

func TestMatchInBothOrders(t *testing.T) {
	for _, subtitleFirst := range []bool{false, true} {
		f := newFixture(t)
		movie, video, srt := newID(), newID(), newID()
		if subtitleFirst {
			f.subtitle(events.FileIndexedV1, srt, "Movie (2020)/Movie (2020).pt-BR.srt")
			if added, removed := f.published(); len(added)+len(removed) != 0 {
				t.Fatalf("a subtitle without item published %v %v", added, removed)
			}
			f.movie(events.MovieAddedV1, movie, video, "Movie (2020)/Movie (2020).mkv")
		} else {
			f.movie(events.MovieAddedV1, movie, video, "Movie (2020)/Movie (2020).mkv")
			if added, removed := f.published(); len(added)+len(removed) != 0 {
				t.Fatalf("an item without subtitle published %v %v", added, removed)
			}
			f.subtitle(events.FileIndexedV1, srt, "Movie (2020)/Movie (2020).pt-BR.srt")
		}

		added, removed := f.published()
		if len(removed) != 0 || len(added) != 1 {
			t.Fatalf("subtitleFirst=%v: added %v, removed %v; want one added", subtitleFirst, added, removed)
		}
		got := added[0]
		want := events.SubtitleEventV1{
			SubtitleID: subtitleID(movie, srt), ItemID: movie, ItemType: "movie", FileID: srt,
			Format: "srt", Language: "pt-BR", Source: "local",
		}
		if got != want {
			t.Errorf("subtitleFirst=%v: event = %+v, want %+v", subtitleFirst, got, want)
		}
	}
}

func TestEpisodeWithForcedSubtitle(t *testing.T) {
	f := newFixture(t)
	ep, video, srt := newID(), newID(), newID()
	f.episode(events.EpisodeAddedV1, ep, video, "Show/Season 1/Show S01E02.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Show/Season 1/Show S01E02.en.forced.srt")

	added, _ := f.published()
	if len(added) != 1 {
		t.Fatalf("added = %v, want 1", added)
	}
	if e := added[0]; e.ItemType != "episode" || e.ItemID != ep || e.Language != "en" || !e.IsForced || e.IsHearingImpaired {
		t.Errorf("event = %+v", e)
	}
}

func TestNoMatchAcrossDirectories(t *testing.T) {
	f := newFixture(t)
	f.movie(events.MovieAddedV1, newID(), newID(), "A/Movie.mkv")
	f.subtitle(events.FileIndexedV1, newID(), "B/Movie.en.srt")
	f.subtitle(events.FileIndexedV1, newID(), "A/Other.en.srt")
	f.subtitle(events.FileIndexedV1, newID(), "A/Movie2.en.srt")
	if added, removed := f.published(); len(added)+len(removed) != 0 {
		t.Fatalf("unexpected matches: %v %v", added, removed)
	}
}

func TestNoMatchAcrossLocations(t *testing.T) {
	f := newFixture(t)
	f.movie(events.MovieAddedV1, newID(), newID(), "Movie.mkv")
	other := &fixture{t: t, svc: f.svc, rec: f.rec, loc: newID()}
	other.subtitle(events.FileIndexedV1, newID(), "Movie.en.srt")
	if added, removed := f.published(); len(added)+len(removed) != 0 {
		t.Fatalf("unexpected matches: %v %v", added, removed)
	}
}

func TestLongestNameWins(t *testing.T) {
	f := newFixture(t)
	short, long, srt := newID(), newID(), newID()
	f.movie(events.MovieAddedV1, short, newID(), "Movie.mkv")
	f.movie(events.MovieAddedV1, long, newID(), "Movie.2.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Movie.2.en.srt")

	added, _ := f.published()
	if len(added) != 1 || added[0].ItemID != long || added[0].Language != "en" {
		t.Fatalf("added = %+v, want the subtitle of the longest name", added)
	}

	// Without the longest, the shorter one claims it: "2.en" is its tags.
	f.removeMovie(long)
	added, removed := f.published()
	if len(removed) != 1 || removed[0].ItemID != long {
		t.Fatalf("removed = %+v, want the subtitle of the removed item", removed)
	}
	if len(added) != 1 || added[0].ItemID != short || added[0].Language != "en" || added[0].Title != "2" {
		t.Fatalf("added = %+v, want the subtitle on the shorter item", added)
	}
}

func TestSameNameMatchesEveryItem(t *testing.T) {
	f := newFixture(t)
	f.movie(events.MovieAddedV1, newID(), newID(), "Movie.mkv")
	f.movie(events.MovieAddedV1, newID(), newID(), "Movie.mp4")
	f.subtitle(events.FileIndexedV1, newID(), "Movie.en.srt")
	if added, _ := f.published(); len(added) != 2 {
		t.Fatalf("added = %+v, want 2", added)
	}
}

func TestTagsComeFromTheNameAfterTheVideo(t *testing.T) {
	f := newFixture(t)
	f.movie(events.MovieAddedV1, newID(), newID(), "Foo.Bar.mkv")
	plain, sdh := newID(), newID()
	f.subtitle(events.FileIndexedV1, plain, "Foo.Bar.srt")
	f.subtitle(events.FileIndexedV1, sdh, "Foo.Bar.en.sdh.srt")

	added, _ := f.published()
	byFile := make(map[uuid.UUID]events.SubtitleEventV1)
	for _, e := range added {
		byFile[e.FileID] = e
	}
	if e := byFile[plain]; e.Language != "" || e.Title != "" {
		t.Errorf("plain subtitle = %+v, want no language nor title", e)
	}
	if e := byFile[sdh]; e.Language != "en" || !e.IsHearingImpaired || e.IsForced {
		t.Errorf("sdh subtitle = %+v", e)
	}
}

func TestSubtitleRemoved(t *testing.T) {
	f := newFixture(t)
	movie, srt := newID(), newID()
	f.movie(events.MovieAddedV1, movie, newID(), "Movie.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Movie.en.srt")
	f.published()

	f.subtitle(events.FileRemovedV1, srt, "Movie.en.srt")
	added, removed := f.published()
	if len(added) != 0 || len(removed) != 1 || removed[0].SubtitleID != subtitleID(movie, srt) ||
		removed[0].ItemID != movie || removed[0].FileID != srt {
		t.Fatalf("added %+v, removed %+v; want the subtitle removed", added, removed)
	}

	// Removing it again is a no-op.
	f.subtitle(events.FileRemovedV1, srt, "Movie.en.srt")
	if added, removed := f.published(); len(added)+len(removed) != 0 {
		t.Fatalf("a duplicated removal published %v %v", added, removed)
	}
}

func TestItemRemoved(t *testing.T) {
	f := newFixture(t)
	movie, srt := newID(), newID()
	f.movie(events.MovieAddedV1, movie, newID(), "Movie.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Movie.en.srt")
	f.published()

	f.removeMovie(movie)
	added, removed := f.published()
	if len(added) != 0 || len(removed) != 1 || removed[0].ItemID != movie {
		t.Fatalf("added %+v, removed %+v; want the subtitle removed", added, removed)
	}

	// The subtitle file is still known: it matches a new item.
	again := newID()
	f.movie(events.MovieAddedV1, again, newID(), "Movie.mkv")
	if added, _ := f.published(); len(added) != 1 || added[0].ItemID != again {
		t.Fatalf("added = %+v, want the subtitle on the new item", added)
	}
}

func TestNotASubtitleAnymore(t *testing.T) {
	f := newFixture(t)
	srt := newID()
	f.movie(events.MovieAddedV1, newID(), newID(), "Movie.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Movie.en.srt")
	f.published()

	f.storage(events.FileMovedV1, srt, "Movie.en.txt", "text/plain")
	if _, removed := f.published(); len(removed) != 1 {
		t.Fatalf("removed = %+v, want the subtitle removed", removed)
	}
}

func TestIgnoresOtherFiles(t *testing.T) {
	f := newFixture(t)
	f.movie(events.MovieAddedV1, newID(), newID(), "Movie.mkv")
	f.storage(events.FileIndexedV1, newID(), "Movie.en.vtt", "text/vtt")
	f.storage(events.FileIndexedV1, newID(), "Movie.mkv", mkv)
	f.storage(events.FileIndexedV1, newID(), "Movie.en.srt", "application/octet-stream")
	if added, removed := f.published(); len(added)+len(removed) != 0 {
		t.Fatalf("unexpected events: %v %v", added, removed)
	}
}

func TestSubtitleMovedToAnotherName(t *testing.T) {
	f := newFixture(t)
	movie, srt := newID(), newID()
	f.movie(events.MovieAddedV1, movie, newID(), "Movie.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Movie.en.srt")
	f.published()

	// Still matches: the attributes changed, so it is added again.
	f.subtitle(events.FileMovedV1, srt, "Movie.pt.srt")
	added, removed := f.published()
	if len(removed) != 0 || len(added) != 1 || added[0].Language != "pt" {
		t.Fatalf("added %+v, removed %+v; want the subtitle updated", added, removed)
	}

	// Moved out of the directory: no match left.
	f.subtitle(events.FileMovedV1, srt, "Other/Movie.pt.srt")
	added, removed = f.published()
	if len(added) != 0 || len(removed) != 1 {
		t.Fatalf("added %+v, removed %+v; want the subtitle removed", added, removed)
	}
}

func TestItemMoved(t *testing.T) {
	f := newFixture(t)
	movie, video, srt := newID(), newID(), newID()
	f.movie(events.MovieAddedV1, movie, video, "Old/Movie.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Old/Movie.en.srt")
	f.subtitle(events.FileIndexedV1, newID(), "New/Movie.fr.srt")
	f.published()

	f.movie(events.MovieUpdatedV1, movie, video, "New/Movie.mkv")
	added, removed := f.published()
	if len(removed) != 1 || removed[0].FileID != srt {
		t.Fatalf("removed = %+v, want the subtitle of the old directory", removed)
	}
	if len(added) != 1 || added[0].Language != "fr" {
		t.Fatalf("added = %+v, want the subtitle of the new directory", added)
	}
}

func TestEventsAreIdempotent(t *testing.T) {
	f := newFixture(t)
	movie, video, srt := newID(), newID(), newID()
	f.movie(events.MovieAddedV1, movie, video, "Movie.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Movie.en.srt")
	if added, _ := f.published(); len(added) != 1 {
		t.Fatalf("added = %+v, want 1", added)
	}

	f.movie(events.MovieAddedV1, movie, video, "Movie.mkv")
	f.movie(events.MovieUpdatedV1, movie, video, "Movie.mkv")
	f.subtitle(events.FileIndexedV1, srt, "Movie.en.srt")
	f.subtitle(events.FileChangedV1, srt, "Movie.en.srt")
	if added, removed := f.published(); len(added)+len(removed) != 0 {
		t.Fatalf("repeated events published %v %v", added, removed)
	}
}

func TestInvalidEventsAreDropped(t *testing.T) {
	f := newFixture(t)
	bad := pubsub.Message{ID: newID().String(), Topic: events.FileIndexedV1, Payload: []byte("{")}
	if err := f.svc.HandleStorageEvent(t.Context(), bad); err != nil {
		t.Errorf("HandleStorageEvent(invalid) = %v, want nil", err)
	}
	bad.Topic = events.MovieAddedV1
	if err := f.svc.HandleLibraryEvent(t.Context(), bad); err != nil {
		t.Errorf("HandleLibraryEvent(invalid) = %v, want nil", err)
	}
}

func TestResyncSubtitles(t *testing.T) {
	f := newFixture(t)
	f.movie(events.MovieAddedV1, newID(), newID(), "A/Movie.mkv")
	f.episode(events.EpisodeAddedV1, newID(), newID(), "B/Show S01E01.mkv")
	f.subtitle(events.FileIndexedV1, newID(), "A/Movie.en.srt")
	f.subtitle(events.FileIndexedV1, newID(), "B/Show S01E01.pt.srt")
	f.published()

	if err := f.svc.ResyncSubtitles(t.Context()); err != nil {
		t.Fatal(err)
	}
	added, removed := f.published()
	if len(removed) != 0 || len(added) != 2 {
		t.Fatalf("added %+v, removed %+v; want 2 added", added, removed)
	}
	types := []string{added[0].ItemType, added[1].ItemType}
	slices.Sort(types)
	if !slices.Equal(types, []string{"episode", "movie"}) {
		t.Errorf("item types = %v", types)
	}
}

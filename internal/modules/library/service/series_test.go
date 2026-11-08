package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/library/events"
	storageevents "github.com/lukerops/mserver/internal/modules/storage/events"
)

// showEvents collects every library event published on bus, in order.
type showEvents chan queue.Message

func subscribeShows(bus *memory.Bus) showEvents {
	got := make(showEvents, 100)
	for _, topic := range []string{
		events.MovieAddedV1, events.MovieUpdatedV1, events.MovieRemovedV1,
		events.SeriesAddedV1, events.SeriesUpdatedV1, events.SeriesRemovedV1,
		events.SeasonAddedV1, events.SeasonRemovedV1,
		events.EpisodeAddedV1, events.EpisodeUpdatedV1, events.EpisodeRemovedV1,
	} {
		bus.Subscribe(topic, func(_ context.Context, msg queue.Message) error {
			got <- msg
			return nil
		})
	}
	return got
}

// next returns the next event, which must be of topic, decoding its data
// into data (a pointer to a struct, or nil), which is zeroed first.
func (c showEvents) next(t *testing.T, topic string, data any) {
	t.Helper()
	select {
	case msg := <-c:
		if msg.Topic != topic {
			t.Fatalf("event %s (%s), want %s", msg.Topic, msg.Payload, topic)
		}
		if data == nil {
			return
		}
		reflect.ValueOf(data).Elem().SetZero()
		e := queue.Envelope[json.RawMessage]{}
		if err := json.Unmarshal(msg.Payload, &e); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(e.Data, data); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s event", topic)
	}
}

// topics returns the topics of the next n events.
func (c showEvents) topics(t *testing.T, n int) []string {
	t.Helper()
	var got []string
	for range n {
		select {
		case msg := <-c:
			got = append(got, msg.Topic)
		case <-time.After(5 * time.Second):
			t.Fatalf("got events %v, want %d", got, n)
		}
	}
	return got
}

func (c showEvents) none(t *testing.T) {
	t.Helper()
	select {
	case msg := <-c:
		t.Fatalf("unexpected event %s: %s", msg.Topic, msg.Payload)
	case <-time.After(1500 * time.Millisecond): // longer than the outbox poll
	}
}

func createShowsLibrary(t *testing.T, s Service, locations ...uuid.UUID) Library {
	t.Helper()
	lib, err := s.CreateLibrary(t.Context(), CreateLibraryRequest{
		Name:        "Shows",
		MediaType:   MediaTypeTVShows,
		LocationIDs: locations,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func listEpisodes(t *testing.T, s Service, q EpisodeQuery) EpisodePage {
	t.Helper()
	page, err := s.ListEpisodes(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func intp(n int) *int { return &n }

func equalTopics(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestCatalogSeries(t *testing.T) {
	bus := memory.New()
	evs := subscribeShows(bus)
	loc := newID()
	s := newTestService(t, bus, newFakeStorage(loc))
	lib := createShowsLibrary(t, s, loc)

	// The first episode creates its series and season, in this order.
	first := newID()
	fileEvent(t, s, storageevents.FileIndexedV1, loc, first, "Breaking Bad (2008)/Season 01/Breaking Bad - S01E01 - Pilot.mkv")
	var series events.SeriesEventV1
	evs.next(t, events.SeriesAddedV1, &series)
	if series.LibraryID != lib.ID || series.Title != "Breaking Bad" || series.Year != 2008 {
		t.Errorf("series added = %+v", series)
	}
	var season events.SeasonEventV1
	evs.next(t, events.SeasonAddedV1, &season)
	if season.SeriesID != series.SeriesID || season.SeasonNumber != 1 || season.LibraryID != lib.ID {
		t.Errorf("season added = %+v", season)
	}
	var episode events.EpisodeEventV1
	evs.next(t, events.EpisodeAddedV1, &episode)
	want := events.EpisodeEventV1{
		EpisodeID: episodeID(first), LibraryID: lib.ID,
		SeriesID: series.SeriesID, SeriesTitle: "Breaking Bad", SeriesYear: 2008,
		SeasonID: season.SeasonID, SeasonNumber: 1, EpisodeNumber: intp(1),
		Title: "Pilot", FileID: first, LocationID: loc,
		Path: "Breaking Bad (2008)/Season 01/Breaking Bad - S01E01 - Pilot.mkv",
	}
	if episode.EpisodeNumber == nil || *episode.EpisodeNumber != 1 || episode.EpisodeEndNumber != nil {
		t.Errorf("episode numbers = %v, %v", episode.EpisodeNumber, episode.EpisodeEndNumber)
	}
	episode.EpisodeNumber = want.EpisodeNumber
	if episode != want {
		t.Errorf("episode added = %+v, want %+v", episode, want)
	}
	if episodeID(first) == movieID(first) {
		t.Error("episode and movie IDs of a file are equal")
	}

	// Other episodes join the series and season, even from other folders.
	double := newID()
	fileEvent(t, s, storageevents.FileIndexedV1, loc, double, "Breaking Bad (2008)/Breaking.Bad.S01E02E03.mkv")
	evs.next(t, events.EpisodeAddedV1, &episode)
	if episode.SeasonID != season.SeasonID || *episode.EpisodeNumber != 2 || episode.EpisodeEndNumber == nil || *episode.EpisodeEndNumber != 3 {
		t.Errorf("multi-episode added = %+v", episode)
	}
	special := newID()
	fileEvent(t, s, storageevents.FileIndexedV1, loc, special, "Breaking Bad (2008)/Specials/Making of.mkv")
	var specials events.SeasonEventV1
	evs.next(t, events.SeasonAddedV1, &specials)
	evs.next(t, events.EpisodeAddedV1, &episode)
	if specials.SeasonNumber != 0 || episode.EpisodeNumber != nil || episode.Title != "Making of" {
		t.Errorf("special = %+v in %+v", episode, specials)
	}

	sr, err := s.GetSeries(t.Context(), series.SeriesID)
	if err != nil || sr.SeasonCount != 2 || sr.EpisodeCount != 3 {
		t.Errorf("series = %+v, %v", sr, err)
	}
	seasons, err := s.ListSeasons(t.Context(), SeasonQuery{SeriesID: series.SeriesID})
	if err != nil || len(seasons) != 2 || seasons[0].Number != 0 || seasons[1].Number != 1 || seasons[1].EpisodeCount != 2 {
		t.Errorf("seasons = %+v, %v", seasons, err)
	}
	page := listEpisodes(t, s, EpisodeQuery{SeriesID: series.SeriesID})
	if page.Total != 3 || page.Episodes[0].ID != episodeID(special) || page.Episodes[1].ID != episodeID(first) ||
		page.Episodes[2].ID != episodeID(double) || page.Episodes[1].SeasonNumber != 1 {
		t.Errorf("episodes = %+v", page)
	}

	// The parents must exist, and the children must belong to them.
	if got, err := s.GetSeason(t.Context(), series.SeriesID, season.SeasonID); err != nil || got.Number != 1 {
		t.Errorf("season of its series = %+v, %v", got, err)
	}
	if got, err := s.GetEpisode(t.Context(), series.SeriesID, season.SeasonID, episodeID(first)); err != nil || got.FileID != first {
		t.Errorf("episode of its season = %+v, %v", got, err)
	}
	if got, err := s.GetEpisode(t.Context(), uuid.Nil, uuid.Nil, episodeID(first)); err != nil || got.FileID != first {
		t.Errorf("episode of any season = %+v, %v", got, err)
	}
	other := newID()
	if _, err := s.GetSeason(t.Context(), other, season.SeasonID); !errors.Is(err, ErrNotFound) {
		t.Errorf("season of another series: got %v, want ErrNotFound", err)
	}
	if _, err := s.GetEpisode(t.Context(), uuid.Nil, specials.SeasonID, episodeID(first)); !errors.Is(err, ErrNotFound) {
		t.Errorf("episode of another season: got %v, want ErrNotFound", err)
	}
	if _, err := s.ListSeasons(t.Context(), SeasonQuery{SeriesID: other}); !errors.Is(err, ErrNotFound) {
		t.Errorf("seasons of an unknown series: got %v, want ErrNotFound", err)
	}
	if _, err := s.ListEpisodes(t.Context(), EpisodeQuery{SeriesID: other, SeasonID: season.SeasonID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("episodes of a season of another series: got %v, want ErrNotFound", err)
	}
	if _, err := s.ListEpisodes(t.Context(), EpisodeQuery{SeasonID: other}); !errors.Is(err, ErrNotFound) {
		t.Errorf("episodes of an unknown season: got %v, want ErrNotFound", err)
	}
	if page := listEpisodes(t, s, EpisodeQuery{SeriesID: series.SeriesID, SeasonID: season.SeasonID}); page.Total != 2 {
		t.Errorf("episodes of the season = %+v", page)
	}

	// Duplicates change nothing.
	fileEvent(t, s, storageevents.FileIndexedV1, loc, first, "Breaking Bad (2008)/Season 01/Breaking Bad - S01E01 - Pilot.mkv")
	evs.none(t)

	// An analysis updates the episode.
	analyzedEvent(t, s, loc, first, MediaInfo{Container: "matroska,webm", DurationMs: 3_480_000})
	evs.next(t, events.EpisodeUpdatedV1, &episode)
	if episode.DurationMs != 3_480_000 {
		t.Errorf("updated after analysis = %+v", episode)
	}

	// Moving the only special to season 2 replaces the specials season.
	fileEvent(t, s, storageevents.FileMovedV1, loc, special, "Breaking Bad (2008)/Season 02/S02E01.mkv")
	if got := evs.topics(t, 3); !equalTopics(got, events.SeasonAddedV1, events.EpisodeUpdatedV1, events.SeasonRemovedV1) {
		t.Errorf("events of the move = %v", got)
	}
	if _, err := s.GetSeason(t.Context(), uuid.Nil, specials.SeasonID); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty season: got %v, want ErrNotFound", err)
	}

	// Removing every episode removes the seasons and the series.
	fileEvent(t, s, storageevents.FileRemovedV1, loc, special, "")
	if got := evs.topics(t, 2); !equalTopics(got, events.EpisodeRemovedV1, events.SeasonRemovedV1) {
		t.Errorf("events of the removal = %v", got)
	}
	fileEvent(t, s, storageevents.FileRemovedV1, loc, first, "")
	evs.next(t, events.EpisodeRemovedV1, nil)
	fileEvent(t, s, storageevents.FileRemovedV1, loc, double, "")
	var removed events.EpisodeRemovedEventV1
	evs.next(t, events.EpisodeRemovedV1, &removed)
	if removed != (events.EpisodeRemovedEventV1{
		EpisodeID: episodeID(double), LibraryID: lib.ID, SeriesID: series.SeriesID, SeasonID: season.SeasonID, FileID: double,
	}) {
		t.Errorf("episode removed = %+v", removed)
	}
	evs.next(t, events.SeasonRemovedV1, nil)
	var seriesRemoved events.SeriesRemovedEventV1
	evs.next(t, events.SeriesRemovedV1, &seriesRemoved)
	if seriesRemoved != (events.SeriesRemovedEventV1{SeriesID: series.SeriesID, LibraryID: lib.ID}) {
		t.Errorf("series removed = %+v", seriesRemoved)
	}
	if _, err := s.GetSeries(t.Context(), series.SeriesID); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty series: got %v, want ErrNotFound", err)
	}
	evs.none(t)
}

func TestSeriesAcrossLocations(t *testing.T) {
	bus := memory.New()
	evs := subscribeShows(bus)
	a, b := newID(), newID()
	s := newTestService(t, bus, newFakeStorage(a, b))
	lib := createShowsLibrary(t, s, a, b)

	// Folders with the same normalized title and year are one series.
	fileEvent(t, s, storageevents.FileIndexedV1, a, newID(), "Avatar - The Last Airbender (2005)/S01E01.mkv")
	fileEvent(t, s, storageevents.FileIndexedV1, b, newID(), "Avatar The Last Airbender (2005)/Season 2/S02E01.mkv")
	// Another year is another series.
	fileEvent(t, s, storageevents.FileIndexedV1, b, newID(), "Avatar The Last Airbender (2024)/S01E01.mkv")
	if got := evs.topics(t, 8); !equalTopics(got,
		events.SeriesAddedV1, events.SeasonAddedV1, events.EpisodeAddedV1,
		events.SeasonAddedV1, events.EpisodeAddedV1,
		events.SeriesAddedV1, events.SeasonAddedV1, events.EpisodeAddedV1) {
		t.Errorf("events = %v", got)
	}

	page, err := s.ListSeries(t.Context(), SeriesQuery{LibraryID: lib.ID, SortBy: SortByYear})
	if err != nil || page.Total != 2 || page.Series[0].Year != 2005 || page.Series[0].SeasonCount != 2 || page.Series[1].Year != 2024 {
		t.Errorf("series = %+v, %v", page, err)
	}
	if _, err := s.ListSeries(t.Context(), SeriesQuery{SortBy: SortByNumber}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("unknown sort: got %v, want ErrInvalidArgument", err)
	}
	if _, err := s.ListEpisodes(t.Context(), EpisodeQuery{SortBy: SortByYear}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("unknown sort: got %v, want ErrInvalidArgument", err)
	}

	// Resync republishes from the series down to the episodes.
	if err := s.ResyncLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	if got := evs.topics(t, 8); !equalTopics(got,
		events.SeriesAddedV1, events.SeriesAddedV1,
		events.SeasonAddedV1, events.SeasonAddedV1, events.SeasonAddedV1,
		events.EpisodeAddedV1, events.EpisodeAddedV1, events.EpisodeAddedV1) {
		t.Errorf("resync events = %v", got)
	}

	// Deleting the library removes from the episodes up to the series.
	if err := s.DeleteLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	if got := evs.topics(t, 8); !equalTopics(got,
		events.EpisodeRemovedV1, events.EpisodeRemovedV1, events.EpisodeRemovedV1,
		events.SeasonRemovedV1, events.SeasonRemovedV1, events.SeasonRemovedV1,
		events.SeriesRemovedV1, events.SeriesRemovedV1) {
		t.Errorf("delete events = %v", got)
	}
	if page, _ := s.ListSeries(t.Context(), SeriesQuery{}); page.Total != 0 {
		t.Errorf("series after delete = %+v", page)
	}
}

func TestFileChangesMediaType(t *testing.T) {
	bus := memory.New()
	evs := subscribeShows(bus)
	movies, shows := newID(), newID()
	s := newTestService(t, bus, newFakeStorage(movies, shows))
	createLibrary(t, s, movies)
	createShowsLibrary(t, s, shows)

	// A file moved from a movie library to a series library becomes an
	// episode, and back.
	file := newID()
	fileEvent(t, s, storageevents.FileIndexedV1, movies, file, "Alien (1979).mkv")
	evs.next(t, events.MovieAddedV1, nil)
	fileEvent(t, s, storageevents.FileMovedV1, shows, file, "Firefly/S01E01.mkv")
	if got := evs.topics(t, 4); !equalTopics(got,
		events.MovieRemovedV1, events.SeriesAddedV1, events.SeasonAddedV1, events.EpisodeAddedV1) {
		t.Errorf("events of the move to the shows = %v", got)
	}
	fileEvent(t, s, storageevents.FileMovedV1, movies, file, "Alien (1979).mkv")
	if got := evs.topics(t, 4); !equalTopics(got,
		events.EpisodeRemovedV1, events.SeasonRemovedV1, events.SeriesRemovedV1, events.MovieAddedV1) {
		t.Errorf("events of the move to the movies = %v", got)
	}
	evs.none(t)
}

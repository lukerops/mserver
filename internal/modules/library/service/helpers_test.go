package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"connectrpc.com/connect/v2"
	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/library/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// fakeStorage is the storage API used by the library: it knows a set of
// locations and records the resyncs. The first failResyncs resyncs fail.
type fakeStorage struct {
	storagev1connect.StorageServiceClient

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

func (s *fakeStorage) GetLocation(_ context.Context, req *storagev1.GetLocationRequest) (*storagev1.GetLocationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := uuid.Parse(req.GetId())
	if err != nil || !s.locations[id] {
		return nil, connect.NewError(connect.CodeNotFound, "location not found")
	}
	return &storagev1.GetLocationResponse{Location: &storagev1.Location{Id: id.String(), Driver: "local"}}, nil
}

func (s *fakeStorage) ResyncLocation(_ context.Context, req *storagev1.ResyncLocationRequest) (*storagev1.ResyncLocationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.attempts <= s.failResyncs {
		return nil, connect.NewError(connect.CodeUnavailable, "unavailable")
	}
	s.resynced = append(s.resynced, uuid.MustParse(req.GetId()))
	return &storagev1.ResyncLocationResponse{}, nil
}

// recorder is a publisher that keeps the messages, in order.
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

// pop removes and returns the oldest message.
func (r *recorder) pop(t *testing.T) pubsub.Message {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.msgs) == 0 {
		t.Fatal("no event published")
	}
	msg := r.msgs[0]
	r.msgs = r.msgs[1:]
	return msg
}

// expect removes the oldest event, which must be of topic.
func (r *recorder) expect(t *testing.T, topic string) {
	t.Helper()
	if msg := r.pop(t); msg.Topic != topic {
		t.Fatalf("event %s (%s), want %s", msg.Topic, msg.Payload, topic)
	}
}

// topics removes the n oldest events and returns their topics.
func (r *recorder) topics(t *testing.T, n int) []string {
	t.Helper()
	got := make([]string, n)
	for i := range got {
		got[i] = r.pop(t).Topic
	}
	return got
}

// none fails if there are events not taken yet.
func (r *recorder) none(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.msgs) > 0 {
		t.Fatalf("unexpected event %s: %s", r.msgs[0].Topic, r.msgs[0].Payload)
	}
}

// next removes the oldest event, which must be of topic, and returns its
// data. Its subject must be the id given by id.
func next[T any](t *testing.T, r *recorder, topic string, id func(T) uuid.UUID) T {
	t.Helper()
	msg := r.pop(t)
	if msg.Topic != topic {
		t.Fatalf("event %s (%s), want %s", msg.Topic, msg.Payload, topic)
	}
	e, err := pubsub.Decode[T](msg)
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != events.SourceLibrary || e.Subject != id(e.Data).String() {
		t.Fatalf("event %s = %+v", msg.Topic, e)
	}
	return e.Data
}

func nextMovie(t *testing.T, r *recorder, topic string) events.MovieEventV1 {
	t.Helper()
	return next(t, r, topic, func(e events.MovieEventV1) uuid.UUID { return e.MovieID })
}

func nextSeries(t *testing.T, r *recorder, topic string) events.SeriesEventV1 {
	t.Helper()
	return next(t, r, topic, func(e events.SeriesEventV1) uuid.UUID { return e.SeriesID })
}

func nextSeason(t *testing.T, r *recorder, topic string) events.SeasonEventV1 {
	t.Helper()
	return next(t, r, topic, func(e events.SeasonEventV1) uuid.UUID { return e.SeasonID })
}

func nextEpisode(t *testing.T, r *recorder, topic string) events.EpisodeEventV1 {
	t.Helper()
	return next(t, r, topic, func(e events.EpisodeEventV1) uuid.UUID { return e.EpisodeID })
}

func newTestService(t *testing.T, pub pubsub.Publisher, storage *fakeStorage) *Service {
	t.Helper()
	cfg := database.Config{Driver: database.SQLite, DataDir: ":memory:"}
	db, err := database.Open(t.Context(), cfg, "library", store.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(store.New(db), pub, storage, slog.Default())
	s.resyncBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	return s
}

func createLibrary(t *testing.T, s *Service, locations ...uuid.UUID) *Library {
	t.Helper()
	return createLibraryOf(t, s, "Movies", MediaTypeMovies, locations)
}

func createShowsLibrary(t *testing.T, s *Service, locations ...uuid.UUID) *Library {
	t.Helper()
	return createLibraryOf(t, s, "Shows", MediaTypeTVShows, locations)
}

func createLibraryOf(t *testing.T, s *Service, name, mediaType string, locations []uuid.UUID) *Library {
	t.Helper()
	lib, err := s.CreateLibrary(t.Context(), &CreateLibraryRequest{
		Name:        name,
		MediaType:   mediaType,
		LocationIDs: locations,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// storageEvent delivers a storage event to the service.
func storageEvent(t *testing.T, s *Service, topic string, data any) {
	t.Helper()
	msg, err := pubsub.Encode(pubsub.NewEnvelope(topic, events.SourceStorage, "", data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleStorageEvent(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
}

func fileEvent(t *testing.T, s *Service, topic string, location, file uuid.UUID, path string) {
	t.Helper()
	storageEvent(t, s, topic, events.FileEventV1{FileID: file, LocationID: location, Path: path})
}

func analyzedEvent(t *testing.T, s *Service, location, file uuid.UUID, info *MediaInfo) {
	t.Helper()
	result, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	storageEvent(t, s, events.FileAnalyzedV1, events.FileAnalyzedEventV1{
		FileID: file, LocationID: location, Analyzer: events.AnalyzerFFprobe, Version: 1, Result: result,
	})
}

func listMovies(t *testing.T, s *Service, q *MovieQuery) *MoviePage {
	t.Helper()
	page, err := s.ListMovies(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func listEpisodes(t *testing.T, s *Service, q *EpisodeQuery) *EpisodePage {
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

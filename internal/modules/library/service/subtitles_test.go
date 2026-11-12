package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
)

func subtitleEvent(t *testing.T, s *Service, topic string, data any) {
	t.Helper()
	msg, err := pubsub.Encode(pubsub.NewEnvelope(topic, events.SourceSubtitles, "", data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleSubtitleEvent(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
}

func addSubtitle(t *testing.T, s *Service, itemType string, item uuid.UUID, language string) uuid.UUID {
	t.Helper()
	id := newID()
	subtitleEvent(t, s, events.SubtitleAddedV1, events.SubtitleEventV1{
		SubtitleID: id, ItemID: item, ItemType: itemType, FileID: newID(),
		Format: "srt", Language: language, Source: events.SubtitleSourceLocal,
	})
	return id
}

func TestExternalSubtitlesOfMovies(t *testing.T) {
	rec := &recorder{}
	loc := newID()
	s := newTestService(t, rec, newFakeStorage(loc))
	createLibrary(t, s, loc)
	file := newID()
	fileEvent(t, s, events.FileIndexedV1, loc, file, "Alien (1979)/Alien.mkv")
	nextMovie(t, rec, events.MovieAddedV1)
	movie := movieID(file)

	pt := addSubtitle(t, s, events.ItemTypeMovie, movie, "pt-BR")
	en := addSubtitle(t, s, events.ItemTypeMovie, movie, "en")
	m, _ := s.GetMovie(t.Context(), uuid.Nil, movie)
	if len(m.ExternalSubtitles) != 2 || m.ExternalSubtitles[0].ID != en || m.ExternalSubtitles[1].ID != pt {
		t.Fatalf("subtitles = %+v, want en then pt-BR", m.ExternalSubtitles)
	}
	if got := m.ExternalSubtitles[1]; got.Format != "srt" || got.Language != "pt-BR" || got.Source != "local" {
		t.Errorf("subtitle = %+v", got)
	}
	// Linking subtitles does not publish events.
	rec.none(t)

	// An added event again replaces the subtitle.
	subtitleEvent(t, s, events.SubtitleAddedV1, events.SubtitleEventV1{
		SubtitleID: pt, ItemID: movie, ItemType: events.ItemTypeMovie, FileID: m.ExternalSubtitles[1].FileID,
		Format: "srt", Language: "pt", Title: "Retimed", Source: "local",
	})
	m, _ = s.GetMovie(t.Context(), uuid.Nil, movie)
	if len(m.ExternalSubtitles) != 2 || m.ExternalSubtitles[1].Title != "Retimed" {
		t.Fatalf("subtitles after replace = %+v", m.ExternalSubtitles)
	}

	// The subtitles survive the events of the file and its analysis.
	fileEvent(t, s, events.FileMovedV1, loc, file, "Alien (1979)/Alien 2.mkv")
	nextMovie(t, rec, events.MovieUpdatedV1)
	analyzedEvent(t, s, loc, file, &MediaInfo{Container: "matroska", DurationMs: 1})
	nextMovie(t, rec, events.MovieUpdatedV1)
	fileEvent(t, s, events.FileChangedV1, loc, file, "Alien (1979)/Alien 2.mkv")
	nextMovie(t, rec, events.MovieUpdatedV1)
	m, _ = s.GetMovie(t.Context(), uuid.Nil, movie)
	if len(m.ExternalSubtitles) != 2 {
		t.Fatalf("subtitles after the file events = %+v", m.ExternalSubtitles)
	}

	subtitleEvent(t, s, events.SubtitleRemovedV1, events.SubtitleRemovedEventV1{SubtitleID: pt, ItemID: movie})
	m, _ = s.GetMovie(t.Context(), uuid.Nil, movie)
	if len(m.ExternalSubtitles) != 1 || m.ExternalSubtitles[0].ID != en {
		t.Fatalf("subtitles after removal = %+v", m.ExternalSubtitles)
	}
	// Duplicates and unknown items are fine.
	subtitleEvent(t, s, events.SubtitleRemovedV1, events.SubtitleRemovedEventV1{SubtitleID: pt, ItemID: movie})
	subtitleEvent(t, s, events.SubtitleRemovedV1, events.SubtitleRemovedEventV1{SubtitleID: pt, ItemID: newID()})
	addSubtitle(t, s, events.ItemTypeMovie, newID(), "en")
	addSubtitle(t, s, "show", movie, "en")
	m, _ = s.GetMovie(t.Context(), uuid.Nil, movie)
	if len(m.ExternalSubtitles) != 1 {
		t.Fatalf("subtitles after the ignored events = %+v", m.ExternalSubtitles)
	}
	rec.none(t)
}

func TestExternalSubtitlesOfEpisodes(t *testing.T) {
	rec := &recorder{}
	loc := newID()
	s := newTestService(t, rec, newFakeStorage(loc))
	createShowsLibrary(t, s, loc)
	file := newID()
	fileEvent(t, s, events.FileIndexedV1, loc, file, "Show (2020)/Season 01/Show S01E01.mkv")
	ep := episodeID(file)

	en := addSubtitle(t, s, events.ItemTypeEpisode, ep, "en")
	e, err := s.store.GetEpisode(t.Context(), ep)
	if err != nil || len(e.ExternalSubtitles) != 1 || e.ExternalSubtitles[0].ID != en {
		t.Fatalf("episode = %+v, %v", e, err)
	}

	fileEvent(t, s, events.FileMovedV1, loc, file, "Show (2020)/Season 01/Show S01E01 renamed.mkv")
	if e, _ = s.store.GetEpisode(t.Context(), ep); len(e.ExternalSubtitles) != 1 {
		t.Fatalf("subtitles after the move = %+v", e.ExternalSubtitles)
	}

	subtitleEvent(t, s, events.SubtitleRemovedV1, events.SubtitleRemovedEventV1{SubtitleID: en, ItemID: ep})
	if e, _ = s.store.GetEpisode(t.Context(), ep); len(e.ExternalSubtitles) != 0 {
		t.Fatalf("subtitles after removal = %+v", e.ExternalSubtitles)
	}
}

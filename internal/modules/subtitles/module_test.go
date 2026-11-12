package subtitles

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/google/uuid"

	subtitlesv1 "github.com/lukerops/mserver/gen/subtitles/v1"
	"github.com/lukerops/mserver/gen/subtitles/v1/subtitlesv1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
	"github.com/lukerops/mserver/internal/pubsub/memory"
)

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func publish[T any](t *testing.T, bus *memory.Bus, typ, source string, subject uuid.UUID, data T) {
	t.Helper()
	msg, err := pubsub.Encode(pubsub.NewEnvelope(typ, source, subject.String(), data))
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
}

func TestModuleMatchesEventsAndResyncs(t *testing.T) {
	bus := memory.New()
	t.Cleanup(func() { bus.Close() })

	server := connect.NewServer()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := New(t.Context(), database.Config{DataDir: t.TempDir()}, server, bus, bus, log)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c := subtitlesv1connect.NewSubtitlesServiceClient(connect.NewClient(connectinprocess.New(server)))

	// Observe what the module publishes.
	added := make(chan events.SubtitleEventV1, 8)
	err = bus.Subscribe(t.Context(), "test", events.SubtitleAddedV1, func(_ context.Context, msg pubsub.Message) error {
		e, err := pubsub.Decode[events.SubtitleEventV1](msg)
		if err != nil {
			return err
		}
		added <- e.Data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	next := func() events.SubtitleEventV1 {
		t.Helper()
		select {
		case e := <-added:
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("no subtitle published")
			return events.SubtitleEventV1{}
		}
	}

	loc, movie, video, srt := newID(), newID(), newID(), newID()
	publish(t, bus, events.MovieAddedV1, events.SourceLibrary, movie, events.MovieEventV1{
		MovieID: movie, FileID: video, LocationID: loc, Path: "Movie (2020)/Movie (2020).mkv",
	})
	publish(t, bus, events.FileIndexedV1, events.SourceStorage, srt, events.FileEventV1{
		FileID: srt, LocationID: loc, Path: "Movie (2020)/Movie (2020).pt-BR.srt", MimeType: "application/x-subrip",
	})

	first := next()
	if first.ItemID != movie || first.FileID != srt || first.Language != "pt-BR" || first.ItemType != "movie" {
		t.Fatalf("event = %+v", first)
	}

	if _, err := c.ResyncSubtitles(t.Context(), &subtitlesv1.ResyncSubtitlesRequest{}); err != nil {
		t.Fatal(err)
	}
	if again := next(); again != first {
		t.Fatalf("resynced event = %+v, want %+v", again, first)
	}
}

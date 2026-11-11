package playback

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/google/uuid"

	playbackv1 "github.com/lukerops/mserver/gen/playback/v1"
	"github.com/lukerops/mserver/gen/playback/v1/playbackv1connect"
	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/kv/memory"
	"github.com/lukerops/mserver/internal/pubsub"
	pubsubmemory "github.com/lukerops/mserver/internal/pubsub/memory"
)

func TestSessionTimeout(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"":    defaultSessionTimeout,
		"10m": 10 * time.Minute,
		"2s":  2 * time.Second,
	} {
		got, err := sessionTimeout(v)
		if err != nil || got != want {
			t.Errorf("sessionTimeout(%q) = %v, %v; want %v", v, got, err, want)
		}
	}
	for _, v := range []string{"often", "0", "-1m"} {
		if _, err := sessionTimeout(v); err == nil {
			t.Errorf("sessionTimeout(%q): expected an error", v)
		}
	}
}

func TestNewRejectsInvalidTimeout(t *testing.T) {
	t.Setenv("MSERVER_PLAYBACK_SESSION_TIMEOUT", "often")
	st := memory.New()
	defer st.Close()
	if m, err := New(t.Context(), connect.NewServer(), pubsubmemory.New(), st, nil); err == nil {
		m.Close()
		t.Fatal("New accepted an invalid session timeout")
	}
}

func start(t *testing.T, bus *pubsubmemory.Bus) playbackv1connect.PlaybackServiceClient {
	t.Helper()
	st := memory.New()
	t.Cleanup(func() { st.Close() })
	server := connect.NewServer()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := New(t.Context(), server, bus, st, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return playbackv1connect.NewPlaybackServiceClient(connect.NewClient(connectinprocess.New(server)))
}

func TestSessionRoundTrip(t *testing.T) {
	ctx := t.Context()
	bus := pubsubmemory.New()
	t.Cleanup(func() { bus.Close() })

	got := make(chan pubsub.Message, 8)
	for _, topic := range []string{events.SessionStartedV1, events.SessionStoppedV1} {
		err := bus.Subscribe(ctx, "test", topic, func(_ context.Context, msg pubsub.Message) error {
			got <- msg
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	c := start(t, bus)

	started, err := c.StartSession(ctx, &playbackv1.StartSessionRequest{
		AccountId:  uuid.NewString(),
		ProfileId:  uuid.NewString(),
		DeviceId:   uuid.NewString(),
		ItemId:     uuid.NewString(),
		FileId:     uuid.NewString(),
		PlayMethod: events.PlayMethodDirectPlay,
		PositionMs: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := started.GetSession().GetId()

	if _, err := c.ReportProgress(ctx, &playbackv1.ReportProgressRequest{Id: id, PositionMs: 5000, Paused: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.HeartbeatSession(ctx, &playbackv1.HeartbeatSessionRequest{Id: id}); err != nil {
		t.Fatal(err)
	}
	resp, err := c.GetSession(ctx, &playbackv1.GetSessionRequest{Id: id})
	if err != nil {
		t.Fatal(err)
	}
	if s := resp.GetSession(); s.GetPositionMs() != 5000 || !s.GetPaused() || s.GetSequence() != 3 {
		t.Fatalf("session = %v", s)
	}
	list, err := c.ListSessions(ctx, &playbackv1.ListSessionsRequest{})
	if err != nil || len(list.GetSessions()) != 1 {
		t.Fatalf("ListSessions = %v, %v", list, err)
	}

	pos := int64(9000)
	if _, err := c.StopSession(ctx, &playbackv1.StopSessionRequest{Id: id, PositionMs: &pos}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetSession(ctx, &playbackv1.GetSessionRequest{Id: id}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("GetSession after stop: got %v, want NotFound", err)
	}

	topics := map[string]bool{}
	for range 2 {
		select {
		case msg := <-got:
			topics[msg.Topic] = true
		case <-time.After(5 * time.Second):
			t.Fatal("events not delivered")
		}
	}
	if !topics[events.SessionStartedV1] || !topics[events.SessionStoppedV1] {
		t.Errorf("topics = %v", topics)
	}
}

package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/kv/memory"
	"github.com/lukerops/mserver/internal/pubsub"
)

const timeout = 5 * time.Minute

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

// received is an event of a session, whatever its type.
type received struct {
	Type              string
	Subject           string
	Sequence          int64
	PositionMs        int64
	IsPaused          bool
	WatchedDurationMs int64
	Reason            string
}

// taken removes the events published so far.
func (r *recorder) taken(t *testing.T) []received {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]received, len(r.msgs))
	for i, msg := range r.msgs {
		env, err := pubsub.Decode[struct {
			Sequence          int64  `json:"sequence"`
			PositionMs        int64  `json:"position_ms"`
			IsPaused          bool   `json:"is_paused"`
			WatchedDurationMs int64  `json:"watched_duration_ms"`
			Reason            string `json:"reason"`
		}](msg)
		if err != nil {
			t.Fatal(err)
		}
		if env.Source != events.SourcePlayback || env.Type != msg.Topic {
			t.Fatalf("event %s = %+v", msg.Topic, env)
		}
		out[i] = received{
			Type:              env.Type,
			Subject:           env.Subject,
			Sequence:          env.Data.Sequence,
			PositionMs:        env.Data.PositionMs,
			IsPaused:          env.Data.IsPaused,
			WatchedDurationMs: env.Data.WatchedDurationMs,
			Reason:            env.Data.Reason,
		}
	}
	r.msgs = nil
	return out
}

// clock is a time that only moves when told.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestService(t *testing.T, st *memory.Store, rec *recorder) (*Service, *clock) {
	t.Helper()
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	s := New(st, rec, timeout, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = clk.now
	return s, clk
}

func newStore(t *testing.T) *memory.Store {
	t.Helper()
	st := memory.New()
	t.Cleanup(func() { st.Close() })
	return st
}

func startRequest() *StartRequest {
	return &StartRequest{
		AccountID:  uuid.New(),
		ProfileID:  uuid.New(),
		DeviceID:   uuid.New(),
		ItemID:     uuid.New(),
		FileID:     uuid.New(),
		PlayMethod: events.PlayMethodDirectPlay,
		PositionMs: 1000,
	}
}

func TestLifecycle(t *testing.T) {
	ctx := t.Context()
	rec := &recorder{}
	s, clk := newTestService(t, newStore(t), rec)

	sess, err := s.Start(ctx, startRequest())
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == uuid.Nil || sess.ID.Version() != 7 || sess.Sequence != 1 || sess.PositionMs != 1000 {
		t.Fatalf("session = %+v", sess)
	}
	if got, err := s.Get(ctx, sess.ID); err != nil || *got != *sess {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	// Plays for 10s, then pauses for 1 minute, then resumes for 5s.
	clk.t = clk.t.Add(10 * time.Second)
	if _, err := s.Progress(ctx, sess.ID, &ProgressRequest{PositionMs: 11000}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Progress(ctx, sess.ID, &ProgressRequest{PositionMs: 11000, Paused: true}); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Minute)
	if err := s.Heartbeat(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Progress(ctx, sess.ID, &ProgressRequest{PositionMs: 11000})
	if err != nil {
		t.Fatal(err)
	}
	if got.WatchedMs != 10000 || got.Paused || got.Sequence != 6 {
		t.Fatalf("session = %+v", got)
	}
	clk.t = clk.t.Add(5 * time.Second)
	if sessions, err := s.List(ctx, 0, 0); err != nil || len(sessions) != 1 || sessions[0].ID != sess.ID {
		t.Fatalf("List = %+v, %v", sessions, err)
	}

	pos := int64(16000)
	if err := s.Stop(ctx, sess.ID, &StopRequest{PositionMs: &pos}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, sess.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after stop: got %v, want ErrNotFound", err)
	}
	if err := s.Stop(ctx, sess.ID, &StopRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("second stop: got %v, want ErrNotFound", err)
	}
	if _, err := s.Progress(ctx, sess.ID, &ProgressRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("progress after stop: got %v, want ErrNotFound", err)
	}

	evs := rec.taken(t)
	want := []string{
		events.SessionStartedV1,
		events.SessionProgressedV1,
		events.SessionProgressedV1, events.SessionPausedV1,
		events.SessionProgressedV1, events.SessionResumedV1,
		events.SessionStoppedV1,
	}
	if len(evs) != len(want) {
		t.Fatalf("events = %+v", evs)
	}
	for i, e := range evs {
		if e.Type != want[i] || e.Sequence != int64(i+1) || e.Subject != sess.ID.String() {
			t.Errorf("event %d = %s #%d (%s), want %s #%d", i, e.Type, e.Sequence, e.Subject, want[i], i+1)
		}
	}
	if !evs[2].IsPaused {
		t.Errorf("progress before pause: %+v", evs[2])
	}
	if stopped := evs[6]; stopped.Reason != events.StopReasonUser ||
		stopped.PositionMs != 16000 || stopped.WatchedDurationMs != 15000 {
		t.Errorf("stopped = %+v", stopped)
	}
}

func TestValidation(t *testing.T) {
	ctx := t.Context()
	rec := &recorder{}
	s, _ := newTestService(t, newStore(t), rec)

	for name, change := range map[string]func(*StartRequest){
		"missing item":   func(r *StartRequest) { r.ItemID = uuid.Nil },
		"missing device": func(r *StartRequest) { r.DeviceID = uuid.Nil },
		"transcode":      func(r *StartRequest) { r.PlayMethod = "transcode" },
		"negative":       func(r *StartRequest) { r.PositionMs = -1 },
	} {
		req := startRequest()
		change(req)
		if _, err := s.Start(ctx, req); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: got %v, want ErrInvalidArgument", name, err)
		}
	}
	if _, err := s.Progress(ctx, uuid.New(), &ProgressRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session: got %v, want ErrNotFound", err)
	}
	if _, err := s.Progress(ctx, uuid.New(), &ProgressRequest{PositionMs: -1}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("negative progress: got %v, want ErrInvalidArgument", err)
	}
	neg := int64(-1)
	if err := s.Stop(ctx, uuid.New(), &StopRequest{PositionMs: &neg}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("negative stop: got %v, want ErrInvalidArgument", err)
	}
	if err := s.Heartbeat(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("heartbeat of unknown session: got %v, want ErrNotFound", err)
	}
	if evs := rec.taken(t); len(evs) != 0 {
		t.Errorf("events = %+v, want none", evs)
	}
}

func TestListPages(t *testing.T) {
	ctx := t.Context()
	s, clk := newTestService(t, newStore(t), &recorder{})

	var ids []uuid.UUID
	for range 5 {
		sess, err := s.Start(ctx, startRequest())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sess.ID)
		clk.t = clk.t.Add(time.Second)
	}

	for _, tt := range []struct {
		name          string
		limit, offset int
		want          []uuid.UUID
	}{
		{"all", 0, 0, ids},
		{"first two", 2, 0, ids[:2]},
		{"middle", 2, 2, ids[2:4]},
		{"offset only", 0, 3, ids[3:]},
		{"past the end", 2, 10, nil},
		{"limit past the end", 10, 4, ids[4:]},
	} {
		got, err := s.List(ctx, tt.limit, tt.offset)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if len(got) != len(tt.want) {
			t.Fatalf("%s: got %d sessions, want %d", tt.name, len(got), len(tt.want))
		}
		for i := range got {
			if got[i].ID != tt.want[i] {
				t.Errorf("%s: session %d = %s, want %s", tt.name, i, got[i].ID, tt.want[i])
			}
		}
	}
}

func TestExpireIdle(t *testing.T) {
	ctx := t.Context()
	rec := &recorder{}
	s, clk := newTestService(t, newStore(t), rec)

	idle, err := s.Start(ctx, startRequest())
	if err != nil {
		t.Fatal(err)
	}
	alive, err := s.Start(ctx, startRequest())
	if err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Minute)
	if err := s.Heartbeat(ctx, idle.ID); err != nil { // watched: 1 minute
		t.Fatal(err)
	}
	clk.t = clk.t.Add(timeout - 30*time.Second)
	if err := s.Heartbeat(ctx, alive.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ExpireIdle(ctx); err != nil || n != 0 {
		t.Fatalf("ExpireIdle before the timeout = %d, %v", n, err)
	}
	clk.t = clk.t.Add(time.Minute)
	if n, err := s.ExpireIdle(ctx); err != nil || n != 1 {
		t.Fatalf("ExpireIdle = %d, %v", n, err)
	}
	if _, err := s.Get(ctx, idle.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("idle session: got %v, want ErrNotFound", err)
	}
	if _, err := s.Get(ctx, alive.ID); err != nil {
		t.Errorf("alive session: %v", err)
	}

	evs := rec.taken(t)
	if len(evs) != 3 {
		t.Fatalf("events = %+v", evs)
	}
	stopped := evs[2]
	if stopped.Type != events.SessionStoppedV1 || stopped.Subject != idle.ID.String() ||
		stopped.Reason != events.StopReasonTimeout ||
		stopped.WatchedDurationMs != time.Minute.Milliseconds() || stopped.Sequence != 2 {
		t.Errorf("stopped = %+v", stopped)
	}
}

// TestSharedStore runs two instances on the same store: any of them
// serves any session, and an idle session is stopped only once.
func TestSharedStore(t *testing.T) {
	ctx := t.Context()
	st := newStore(t)
	rec := &recorder{}
	a, clkA := newTestService(t, st, rec)
	b, clkB := newTestService(t, st, rec)

	sess, err := a.Start(ctx, startRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Progress(ctx, sess.ID, &ProgressRequest{PositionMs: 2000}); err != nil {
		t.Fatal(err)
	}
	clkA.t = clkA.t.Add(2 * timeout)
	clkB.t = clkA.t
	na, errA := a.ExpireIdle(ctx)
	nb, errB := b.ExpireIdle(ctx)
	if errA != nil || errB != nil || na+nb != 1 {
		t.Fatalf("ExpireIdle = %d, %v and %d, %v", na, errA, nb, errB)
	}
	// started, progressed and a single stopped.
	if evs := rec.taken(t); len(evs) != 3 {
		t.Fatalf("events = %+v", evs)
	}
}

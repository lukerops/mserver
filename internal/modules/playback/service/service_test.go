package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	database "github.com/lukerops/mserver/internal/infra/database/sqlite"
	kvmemory "github.com/lukerops/mserver/internal/infra/kv/memory"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/playback/events"
)

const timeout = 5 * time.Minute

// received is an event published on the bus.
type received struct {
	Type string `json:"type"`
	Data struct {
		Sequence          int64  `json:"sequence"`
		PositionMs        int64  `json:"position_ms"`
		IsPaused          bool   `json:"is_paused"`
		WatchedDurationMs int64  `json:"watched_duration_ms"`
		Reason            string `json:"reason"`
	} `json:"data"`
}

// recorder records the playback events published on a bus.
type recorder struct {
	mu     sync.Mutex
	events []received
}

func (r *recorder) wait(t *testing.T, n int) []received {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		r.mu.Lock()
		got := append([]received(nil), r.events...)
		r.mu.Unlock()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d events, want %d: %+v", len(got), n, got)
		}
	}
}

// clock is a time that only moves when told.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestService(t *testing.T, st *kvmemory.Store) (*service, *recorder, *clock) {
	t.Helper()
	ctx := t.Context()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "playback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	bus := memory.New()
	rec := &recorder{}
	for _, typ := range []string{
		events.SessionStartedV1, events.SessionProgressedV1, events.SessionPausedV1,
		events.SessionResumedV1, events.SessionStoppedV1,
	} {
		bus.Subscribe(typ, func(_ context.Context, msg queue.Message) error {
			var e received
			if err := json.Unmarshal(msg.Payload, &e); err != nil {
				return err
			}
			rec.mu.Lock()
			rec.events = append(rec.events, e)
			rec.mu.Unlock()
			return nil
		})
	}
	ob, err := outbox.New(ctx, db, bus, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	ob.Start(ctx)
	t.Cleanup(ob.Stop)

	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	s := New(st, ob, slog.Default(), timeout).(*service)
	s.now = clk.now
	return s, rec, clk
}

func startRequest() StartRequest {
	return StartRequest{
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
	s, rec, clk := newTestService(t, kvmemory.New())

	sess, err := s.Start(ctx, startRequest())
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == uuid.Nil || sess.ID.Version() != 7 || sess.Sequence != 1 || sess.PositionMs != 1000 {
		t.Fatalf("session = %+v", sess)
	}
	if got, err := s.Get(ctx, sess.ID); err != nil || got != sess {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	// Plays for 10s, then pauses for 1 minute, then resumes for 5s.
	clk.t = clk.t.Add(10 * time.Second)
	if _, err := s.Progress(ctx, sess.ID, ProgressRequest{PositionMs: 11000}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Progress(ctx, sess.ID, ProgressRequest{PositionMs: 11000, Paused: true}); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Minute)
	if err := s.Heartbeat(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Progress(ctx, sess.ID, ProgressRequest{PositionMs: 11000})
	if err != nil {
		t.Fatal(err)
	}
	if got.WatchedMs != 10000 || got.Paused || got.Sequence != 6 {
		t.Fatalf("session = %+v", got)
	}
	clk.t = clk.t.Add(5 * time.Second)
	if sessions, err := s.List(ctx); err != nil || len(sessions) != 1 || sessions[0].ID != sess.ID {
		t.Fatalf("List = %+v, %v", sessions, err)
	}

	pos := int64(16000)
	if err := s.Stop(ctx, sess.ID, StopRequest{PositionMs: &pos}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, sess.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after stop: got %v, want ErrNotFound", err)
	}
	if err := s.Stop(ctx, sess.ID, StopRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("second stop: got %v, want ErrNotFound", err)
	}
	if _, err := s.Progress(ctx, sess.ID, ProgressRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("progress after stop: got %v, want ErrNotFound", err)
	}

	evs := rec.wait(t, 7)
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
		if e.Type != want[i] || e.Data.Sequence != int64(i+1) {
			t.Errorf("event %d = %s #%d, want %s #%d", i, e.Type, e.Data.Sequence, want[i], i+1)
		}
	}
	if !evs[2].Data.IsPaused {
		t.Errorf("progress before pause: %+v", evs[2])
	}
	if stopped := evs[6].Data; stopped.Reason != events.StopReasonUser ||
		stopped.PositionMs != 16000 || stopped.WatchedDurationMs != 15000 {
		t.Errorf("stopped = %+v", stopped)
	}
}

func TestValidation(t *testing.T) {
	ctx := t.Context()
	s, _, _ := newTestService(t, kvmemory.New())

	for name, change := range map[string]func(*StartRequest){
		"missing item":   func(r *StartRequest) { r.ItemID = uuid.Nil },
		"missing device": func(r *StartRequest) { r.DeviceID = uuid.Nil },
		"transcode":      func(r *StartRequest) { r.PlayMethod = "transcode" },
		"negative":       func(r *StartRequest) { r.PositionMs = -1 },
	} {
		req := startRequest()
		change(&req)
		if _, err := s.Start(ctx, req); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: got %v, want ErrInvalidArgument", name, err)
		}
	}
	if _, err := s.Progress(ctx, uuid.New(), ProgressRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session: got %v, want ErrNotFound", err)
	}
	if err := s.Heartbeat(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("heartbeat of unknown session: got %v, want ErrNotFound", err)
	}
}

func TestExpireIdle(t *testing.T) {
	ctx := t.Context()
	s, rec, clk := newTestService(t, kvmemory.New())

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

	evs := rec.wait(t, 3)
	stopped := evs[2]
	if stopped.Type != events.SessionStoppedV1 || stopped.Data.Reason != events.StopReasonTimeout ||
		stopped.Data.WatchedDurationMs != time.Minute.Milliseconds() || stopped.Data.Sequence != 2 {
		t.Errorf("stopped = %+v", stopped)
	}
}

// TestSharedStore runs two instances on the same store: any of them
// serves any session, and an idle session is stopped only once.
func TestSharedStore(t *testing.T) {
	ctx := t.Context()
	st := kvmemory.New()
	a, recA, clkA := newTestService(t, st)
	b, recB, clkB := newTestService(t, st)

	sess, err := a.Start(ctx, startRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Progress(ctx, sess.ID, ProgressRequest{PositionMs: 2000}); err != nil {
		t.Fatal(err)
	}
	clkA.t = clkA.t.Add(2 * timeout)
	clkB.t = clkA.t
	na, errA := a.ExpireIdle(ctx)
	nb, errB := b.ExpireIdle(ctx)
	if errA != nil || errB != nil || na+nb != 1 {
		t.Fatalf("ExpireIdle = %d, %v and %d, %v", na, errA, nb, errB)
	}
	evsA := recA.wait(t, 2)
	evsB := recB.wait(t, 1)
	time.Sleep(1500 * time.Millisecond) // a second relay round
	recA.mu.Lock()
	recB.mu.Lock()
	defer recA.mu.Unlock()
	defer recB.mu.Unlock()
	// started, progressed and a single stopped.
	if len(recA.events)+len(recB.events) != 3 {
		t.Fatalf("events = %+v and %+v", evsA, evsB)
	}
}

package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lukerops/mserver/internal/infra/database"
	dbsqlite "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/queue"
)

// fakePublisher records published messages and fails while failing is set.
type fakePublisher struct {
	mu      sync.Mutex
	failing bool
	got     []string
}

func (p *fakePublisher) Publish(_ context.Context, msg queue.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failing {
		return errors.New("unavailable")
	}
	var e queue.Envelope[string]
	if err := json.Unmarshal(msg.Payload, &e); err != nil {
		return err
	}
	p.got = append(p.got, e.Data)
	return nil
}

func (p *fakePublisher) setFailing(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failing = v
}

func (p *fakePublisher) published() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.got...)
}

func newTestOutbox(t *testing.T, pub queue.Publisher) (*Outbox, *sql.DB) {
	t.Helper()
	db, err := dbsqlite.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	o, err := New(t.Context(), db, pub, nil)
	if err != nil {
		t.Fatal(err)
	}
	o.poll = 10 * time.Millisecond
	o.Start(t.Context())
	t.Cleanup(o.Stop)
	return o, db
}

func publish(t *testing.T, o *Outbox, db *sql.DB, commit bool, events ...string) {
	t.Helper()
	rollback := errors.New("rollback")
	err := database.WithTx(t.Context(), db, func(ctx context.Context) error {
		for _, e := range events {
			if err := o.Publish(ctx, queue.NewEnvelope("topic", "test", "", e)); err != nil {
				return err
			}
		}
		if !commit {
			return rollback
		}
		return nil
	})
	if err != nil && !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, pub *fakePublisher, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := pub.published()
		if len(got) >= len(want) {
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("published %v, want %v", got, want)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("published %v, want %v", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPublishAfterCommit(t *testing.T) {
	pub := &fakePublisher{}
	o, db := newTestOutbox(t, pub)

	publish(t, o, db, false, "rolled back")
	publish(t, o, db, true, "a", "b")
	waitFor(t, pub, "a", "b")
}

func TestFailedEventIsRetriedInOrder(t *testing.T) {
	pub := &fakePublisher{failing: true}
	o, db := newTestOutbox(t, pub)

	publish(t, o, db, true, "a", "b")
	time.Sleep(50 * time.Millisecond)
	pub.setFailing(false)
	waitFor(t, pub, "a", "b")
}

func TestPublishManyWithoutTransaction(t *testing.T) {
	pub := &fakePublisher{}
	o, _ := newTestOutbox(t, pub)

	events := []queue.Event{
		queue.NewEnvelope("topic", "test", "", "a"),
		queue.NewEnvelope("topic", "test", "", "b"),
		queue.NewEnvelope("topic", "test", "", "c"),
	}
	if err := o.Publish(t.Context(), events...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, pub, "a", "b", "c")
}

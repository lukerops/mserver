// Package outbox implements the transactional outbox: events are saved in
// the same transaction as the state change that produced them and a
// background relay publishes them afterwards, in order.
//
// Each module creates its own Outbox on its own database. The outbox
// table is created by New.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lukerops/mserver/internal/infra/database"
	"github.com/lukerops/mserver/internal/infra/logging"
	"github.com/lukerops/mserver/internal/infra/queue"
)

const (
	batchSize = 100
	// defaultPoll is how often the relay checks the table. It is also the
	// delay before a failed event is retried.
	defaultPoll = time.Second
)

// Outbox saves events and relays them to a publisher.
type Outbox struct {
	db   *sql.DB
	pub  queue.Publisher
	log  *slog.Logger
	poll time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates the outbox table in db if needed.
func New(ctx context.Context, db *sql.DB, pub queue.Publisher, log *slog.Logger) (*Outbox, error) {
	if log == nil {
		log = slog.Default()
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS outbox (
		id      TEXT PRIMARY KEY, -- event ID (UUIDv7): orders the events
		topic   TEXT NOT NULL,
		payload BLOB NOT NULL
	)`); err != nil {
		return nil, fmt.Errorf("outbox: create table: %w", err)
	}
	return &Outbox{db: db, pub: pub, log: log, poll: defaultPoll}, nil
}

// Publish saves events, encoded as JSON, to be published on their topics
// in the given order. Event IDs must be UUIDv7: they order the outbox.
//
// Call it inside database.WithTx on the same db to save the events
// atomically with the state change: they are published after the commit
// and discarded on rollback. Without a transaction in ctx, the events are
// saved in a transaction of their own.
func (o *Outbox) Publish(ctx context.Context, events ...queue.Event) error {
	return database.WithTx(ctx, o.db, func(ctx context.Context) error {
		conn := database.ConnFrom(ctx, o.db)
		for _, e := range events {
			payload, err := json.Marshal(e)
			if err != nil {
				return fmt.Errorf("outbox: encode %s: %w", e.Topic(), err)
			}
			if _, err := conn.ExecContext(ctx,
				`INSERT INTO outbox (id, topic, payload) VALUES (?, ?, ?)`, e.EventID(), e.Topic(), payload,
			); err != nil {
				return fmt.Errorf("outbox: save %s: %w", e.EventID(), err)
			}
			// Logged with the context of the caller: the request ID ties the
			// event to the request that produced it. The event is published
			// only if the transaction commits ("outbox: event published").
			o.log.DebugContext(ctx, "outbox: event queued", "event_id", e.EventID(), "topic", e.Topic())
		}
		return nil
	})
}

// Start runs the relay in the background until Stop.
func (o *Outbox) Start(ctx context.Context) {
	ctx, o.cancel = context.WithCancel(context.WithoutCancel(ctx))
	o.wg.Go(func() { o.relay(ctx) })
}

// Stop stops the relay and waits for it. Pending events stay in the table
// and are published on the next Start.
func (o *Outbox) Stop() {
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()
}

func (o *Outbox) relay(ctx context.Context) {
	ticker := time.NewTicker(o.poll)
	defer ticker.Stop()
	for {
		if err := o.publishPending(ctx); err != nil && ctx.Err() == nil {
			o.log.Warn("outbox: publish failed, retrying", "in", o.poll, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// publishPending publishes the saved events in order, deleting each one
// after it is published. It stops at the first failure so that the order
// is kept; the failed event is retried on the next round.
func (o *Outbox) publishPending(ctx context.Context) error {
	for {
		entries, err := o.pending(ctx)
		if err != nil || len(entries) == 0 {
			return err
		}
		for _, e := range entries {
			// The handlers run with the event ID as request ID: their logs,
			// and the module calls they make, carry the ID of the event.
			ctx := logging.WithRequestID(ctx, e.id)
			if err := o.pub.Publish(ctx, e.msg); err != nil {
				return fmt.Errorf("event %s (%s): %w", e.id, e.msg.Topic, err)
			}
			if _, err := o.db.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, e.id); err != nil {
				return fmt.Errorf("outbox: delete %s: %w", e.id, err)
			}
			o.logPublished(ctx, e)
		}
	}
}

// logPublished logs a published event with its subject and data, at
// debug level.
func (o *Outbox) logPublished(ctx context.Context, e entry) {
	if !o.log.Enabled(ctx, slog.LevelDebug) {
		return
	}
	var env struct {
		Subject string          `json:"subject"`
		Time    time.Time       `json:"time"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(e.msg.Payload, &env)
	attrs := []any{"event_id", e.id, "topic", e.msg.Topic, "subject", env.Subject}
	if !env.Time.IsZero() {
		attrs = append(attrs, "delay", time.Since(env.Time).Round(time.Millisecond))
	}
	attrs = append(attrs, "data", string(env.Data))
	o.log.DebugContext(ctx, "outbox: event published", attrs...)
}

// entry is a saved event.
type entry struct {
	id  string
	msg queue.Message
}

func (o *Outbox) pending(ctx context.Context) ([]entry, error) {
	rows, err := o.db.QueryContext(ctx,
		`SELECT id, topic, payload FROM outbox ORDER BY id LIMIT ?`, batchSize)
	if err != nil {
		return nil, fmt.Errorf("outbox: read: %w", err)
	}
	defer rows.Close()

	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.msg.Topic, &e.msg.Payload); err != nil {
			return nil, fmt.Errorf("outbox: read: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

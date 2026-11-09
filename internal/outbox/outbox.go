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

	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/logger"
	"github.com/lukerops/mserver/internal/pubsub"
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
	pub  pubsub.Publisher
	poll time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates the outbox table in db if needed.
func New(ctx context.Context, db *sql.DB, pub pubsub.Publisher) (*Outbox, error) {
	if _, err := db.ExecContext(ctx, creationQuery); err != nil {
		return nil, fmt.Errorf("outbox: create table: %w", err)
	}

	outbox := &Outbox{
		db:   db,
		pub:  pub,
		poll: defaultPoll,
	}

	ctx, outbox.cancel = context.WithCancel(context.WithoutCancel(ctx))
	outbox.wg.Go(func() { outbox.relay(ctx) })

	return outbox, nil
}

// Publish saves events, encoded as JSON, to be published on their topics
// in the given order. Event IDs must be UUIDv7: they order the outbox.
//
// Call it inside database.WithTx on the same db to save the events
// atomically with the state change: they are published after the commit
// and discarded on rollback. Without a transaction in ctx, the events are
// saved in a transaction of their own.
func (o *Outbox) Publish(ctx context.Context, events ...pubsub.Event) error {
	return database.WithTx(ctx, o.db, func(ctx context.Context) error {
		log := logger.FromContext(ctx)
		conn := database.ConnFrom(ctx, o.db)
		for _, e := range events {
			payload, err := json.Marshal(e)
			if err != nil {
				return fmt.Errorf("outbox: encode %s: %w", e.Topic(), err)
			}

			if _, err := conn.ExecContext(ctx, insertQuery, e.EventID(), e.Topic(), payload); err != nil {
				return fmt.Errorf("outbox: save %s: %w", e.EventID(), err)
			}

			// Logged with the context of the caller: the request ID ties the
			// event to the request that produced it. The event is published
			// only if the transaction commits ("outbox: event published").
			log.DebugContext(ctx, "outbox: event published",
				slog.String("event_id", e.EventID()),
				slog.String("topic", e.Topic()),
			)
		}
		return nil
	})
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
	log := logger.FromContext(ctx)
	ticker := time.NewTicker(o.poll)
	defer ticker.Stop()
	for {
		if err := o.publishPending(ctx); err != nil {
			log.Warn("outbox: publish failed, retrying",
				slog.Int64("in", int64(o.poll)),
				slog.String("error", err.Error()),
			)
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
		entries, err := o.pendingJobs(ctx)
		if err != nil {
			return err
		}
		for _, e := range entries {
			// The handlers run with the event ID as request ID: their logs,
			// and the module calls they make, carry the ID of the event.
			ctx := logger.With(ctx, slog.String("event_id", e.id))
			if err := o.pub.Publish(ctx, e.msg); err != nil {
				return fmt.Errorf("outbox: failed to publish event %s (%s): %w", e.id, e.msg.Topic, err)
			}

			if _, err := o.db.ExecContext(ctx, deleteJob, e.id); err != nil {
				return fmt.Errorf("outbox: failed to delete a published event (%s): %w", e.id, err)
			}

			log := logger.FromContext(ctx)
			if log.Enabled(ctx, slog.LevelDebug) {
				var env struct {
					Subject string          `json:"subject"`
					Time    time.Time       `json:"time"`
					Data    json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(e.msg.Payload, &env); err != nil {
					continue
				}

				log.Debug("outbox: event published",
					slog.String("event_id", e.id),
					slog.String("topic", e.msg.Topic),
					slog.String("subject", env.Subject),
					slog.Int64("delay", int64(time.Since(env.Time).Round(time.Millisecond))),
					slog.Any("data", env.Data),
				)
			}
		}
	}
}

// entry is a saved event.
type entry struct {
	id  string
	msg pubsub.Message
}

func (o *Outbox) pendingJobs(ctx context.Context) ([]entry, error) {
	rows, err := o.db.QueryContext(ctx, getPendpings, batchSize)
	if err != nil {
		return nil, fmt.Errorf("outbox: failed to list pending jobs: %w", err)
	}
	defer rows.Close()

	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.msg.Topic, &e.msg.Payload); err != nil {
			return nil, fmt.Errorf("outbox: failed to list pending jobs: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

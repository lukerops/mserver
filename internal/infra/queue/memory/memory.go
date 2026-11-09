// Package memory is an in-process message bus that calls the handlers of
// a topic synchronously.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"

	"github.com/lukerops/mserver/internal/infra/queue"
)

// Bus implements queue.Publisher and queue.Subscriber.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]queue.Handler
	log      *slog.Logger
}

var (
	_ queue.Publisher  = (*Bus)(nil)
	_ queue.Subscriber = (*Bus)(nil)
)

// Option configures a Bus.
type Option func(*Bus)

// WithLogger logs the deliveries (debug) and the failures of handlers
// (warn). Without it, nothing is logged.
func WithLogger(log *slog.Logger) Option {
	return func(b *Bus) { b.log = log }
}

func New(opts ...Option) *Bus {
	b := &Bus{handlers: make(map[string][]queue.Handler)}
	for _, opt := range opts {
		opt(b)
	}
	if b.log == nil {
		b.log = slog.New(slog.DiscardHandler)
	}
	return b
}

func (b *Bus) Subscribe(topic string, h queue.Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], h)
	b.log.Debug("queue: subscribed", "topic", topic, "handlers", len(b.handlers[topic]))
}

// Publish calls every handler of the topic and joins their errors.
func (b *Bus) Publish(ctx context.Context, msg queue.Message) error {
	b.mu.RLock()
	handlers := slices.Clone(b.handlers[msg.Topic])
	b.mu.RUnlock()

	debug := b.log.Enabled(ctx, slog.LevelDebug)
	var id string
	if debug {
		id = eventID(msg)
	}
	if len(handlers) == 0 {
		b.log.DebugContext(ctx, "queue: no handler for event", "topic", msg.Topic, "event_id", id)
		return nil
	}
	b.log.DebugContext(ctx, "queue: delivering event", "topic", msg.Topic, "event_id", id, "handlers", len(handlers))

	var errs []error
	for i, h := range handlers {
		m := queue.Message{Topic: msg.Topic, Payload: slices.Clone(msg.Payload)}
		if err := h(ctx, m); err != nil {
			if !debug {
				id = eventID(msg)
			}
			b.log.WarnContext(ctx, "queue: handler failed", "topic", msg.Topic, "event_id", id, "handler", i, "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// eventID reads the ID of a CloudEvents payload, for the logs.
func eventID(msg queue.Message) string {
	var env struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(msg.Payload, &env)
	return env.ID
}

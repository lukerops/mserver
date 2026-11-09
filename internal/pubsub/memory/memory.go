// Package memory is an in-process message bus that calls the handlers of
// a topic synchronously.
package memory

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"

	"github.com/lukerops/mserver/internal/pubsub"
)

// Bus implements queue.Publisher and queue.Subscriber.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]pubsub.Handler
	log      *slog.Logger
}

func New() *Bus {
	b := &Bus{
		handlers: make(map[string][]pubsub.Handler),
		log:      slog.Default().With(slog.String("component", "pubsub")),
	}

	return b
}

func (b *Bus) Subscribe(topic string, h pubsub.Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers[topic] = append(b.handlers[topic], h)
	b.log.Debug("pubsub: topic subscribed",
		slog.String("topic", topic),
		slog.Int("handlers", len(b.handlers[topic])),
	)
}

// Publish calls every handler of the topic and joins their errors.
func (b *Bus) Publish(ctx context.Context, msg pubsub.Message) error {
	b.mu.RLock()
	handlers := slices.Clone(b.handlers[msg.Topic])
	b.mu.RUnlock()

	if len(handlers) == 0 {
		b.log.DebugContext(ctx, "pubsub: no handler for event",
			slog.String("topic", msg.Topic),
		)
		return nil
	}
	b.log.DebugContext(ctx, "pubsub: delivering event",
		slog.String("topic", msg.Topic),
		slog.Int("handlers", len(handlers)),
	)

	var errs []error
	for _, h := range handlers {
		m := pubsub.Message{
			Topic:   msg.Topic,
			Payload: slices.Clone(msg.Payload),
		}

		if err := h(ctx, m); err != nil {
			b.log.WarnContext(ctx, "pubsub: handler failed",
				slog.String("topic", msg.Topic),
				slog.String("error", err.Error()),
			)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

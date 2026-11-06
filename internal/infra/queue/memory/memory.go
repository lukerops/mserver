// Package memory is an in-process message bus that calls the handlers of
// a topic synchronously.
package memory

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/lukerops/mserver/internal/infra/queue"
)

// Bus implements queue.Publisher and queue.Subscriber.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]queue.Handler
}

var (
	_ queue.Publisher  = (*Bus)(nil)
	_ queue.Subscriber = (*Bus)(nil)
)

func New() *Bus {
	return &Bus{handlers: make(map[string][]queue.Handler)}
}

func (b *Bus) Subscribe(topic string, h queue.Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], h)
}

// Publish calls every handler of the topic and joins their errors.
func (b *Bus) Publish(ctx context.Context, msg queue.Message) error {
	b.mu.RLock()
	handlers := slices.Clone(b.handlers[msg.Topic])
	b.mu.RUnlock()

	var errs []error
	for _, h := range handlers {
		m := queue.Message{Topic: msg.Topic, Payload: slices.Clone(msg.Payload)}
		if err := h(ctx, m); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

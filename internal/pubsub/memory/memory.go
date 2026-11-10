// Package memory is an in-process message bus for the monolith.
//
// Publish returns as soon as the message is handed to the consumers; each
// consumer handles it in the background. When a handler fails, the delivery
// is retried with a short exponential backoff (100ms, 500ms, 2s, 5s) and
// then given up: the Resync of the producer is what reconciles the state
// after that.
package memory

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/lukerops/mserver/internal/pubsub"
)

// defaultBackoff is the wait before each redelivery of a failed message.
var defaultBackoff = []time.Duration{
	100 * time.Millisecond,
	500 * time.Millisecond,
	2 * time.Second,
	5 * time.Second,
}

// ErrClosed is returned by Publish and Subscribe after Close.
var ErrClosed = errors.New("pubsub: bus closed")

// subscription is a handler registered by a consumer for a topic.
type subscription struct {
	ctx context.Context
	h   pubsub.Handler
}

// group is the set of replicas of one consumer on one topic. Messages are
// handed to them in turn.
type group struct {
	subs []*subscription
	next int
}

var (
	_ pubsub.Publisher  = (*Bus)(nil)
	_ pubsub.Subscriber = (*Bus)(nil)
)

// Bus implements pubsub.Publisher and pubsub.Subscriber.
type Bus struct {
	mu     sync.Mutex
	topics map[string]map[string]*group // topic -> consumer -> group
	closed bool

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	backoff []time.Duration
	log     *slog.Logger
}

// New returns a bus with no subscriptions.
func New() *Bus {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bus{
		topics:  make(map[string]map[string]*group),
		ctx:     ctx,
		cancel:  cancel,
		backoff: defaultBackoff,
		log:     slog.Default().With(slog.String("component", "pubsub")),
	}
}

// Close stops the redeliveries, waits for the handlers running and rejects
// further use of the bus.
func (b *Bus) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()

	b.cancel()
	b.wg.Wait()
	return nil
}

// Subscribe registers h for topic on behalf of consumer until ctx is
// cancelled.
func (b *Bus) Subscribe(ctx context.Context, consumer, topic string, h pubsub.Handler) error {
	if consumer == "" || topic == "" || h == nil {
		return errors.New("pubsub: consumer, topic and handler are required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrClosed
	}

	consumers := b.topics[topic]
	if consumers == nil {
		consumers = make(map[string]*group)
		b.topics[topic] = consumers
	}
	g := consumers[consumer]
	if g == nil {
		g = &group{}
		consumers[consumer] = g
	}
	sub := &subscription{ctx: ctx, h: h}
	g.subs = append(g.subs, sub)

	b.log.Debug("pubsub: topic subscribed",
		slog.String("consumer", consumer),
		slog.String("topic", topic),
		slog.Int("replicas", len(g.subs)),
	)

	// Drop the subscription when its context ends.
	context.AfterFunc(ctx, func() { b.unsubscribe(topic, consumer, sub) })
	return nil
}

func (b *Bus) unsubscribe(topic, consumer string, sub *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	g := b.topics[topic][consumer]
	if g == nil {
		return
	}
	g.subs = slices.DeleteFunc(g.subs, func(s *subscription) bool { return s == sub })
	if len(g.subs) == 0 {
		delete(b.topics[topic], consumer)
	}
}

// pick returns the replica of consumer that must handle the next message of
// topic, or nil if there is none.
func (b *Bus) pick(topic, consumer string) *subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	g := b.topics[topic][consumer]
	if g == nil || len(g.subs) == 0 {
		return nil
	}
	sub := g.subs[g.next%len(g.subs)]
	g.next++
	return sub
}

// Publish delivers msg to every consumer subscribed to its topic.
func (b *Bus) Publish(ctx context.Context, msg pubsub.Message) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	consumers := make([]string, 0, len(b.topics[msg.Topic]))
	for consumer := range b.topics[msg.Topic] {
		consumers = append(consumers, consumer)
	}
	// Registered under the lock, so that Close cannot miss the delivery.
	b.wg.Add(len(consumers))
	b.mu.Unlock()

	if len(consumers) == 0 {
		b.log.DebugContext(ctx, "pubsub: no consumer for event", slog.String("topic", msg.Topic))
		return nil
	}

	// The delivery outlives the request that published the event, but keeps
	// its values (e.g. the logger).
	dctx := context.WithoutCancel(ctx)
	for _, consumer := range consumers {
		go func() {
			defer b.wg.Done()
			b.deliver(dctx, consumer, msg)
		}()
	}
	return nil
}

// deliver hands msg to a replica of consumer, retrying on failure.
func (b *Bus) deliver(ctx context.Context, consumer string, msg pubsub.Message) {
	var err error
	for _, backoff := range b.backoff {
		sub := b.pick(msg.Topic, consumer)
		if sub == nil || sub.ctx.Err() != nil {
			return // the consumer is gone
		}

		err = sub.h(ctx, pubsub.Message{
			ID:      msg.ID,
			Topic:   msg.Topic,
			Payload: slices.Clone(msg.Payload),
		})
		if err == nil {
			return
		}

		b.log.WarnContext(ctx, "pubsub: handler failed, will retry",
			slog.String("consumer", consumer),
			slog.String("topic", msg.Topic),
			slog.String("id", msg.ID),
			slog.Duration("retry_in", backoff),
			slog.String("error", err.Error()),
		)

		time.Sleep(backoff)
	}

	b.log.ErrorContext(ctx, "pubsub: event given up",
		slog.String("consumer", consumer),
		slog.String("topic", msg.Topic),
		slog.String("id", msg.ID),
		slog.String("error", err.Error()),
	)
}

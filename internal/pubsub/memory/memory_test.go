package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lukerops/mserver/internal/pubsub"
)

const wait = 2 * time.Second

func newBus(t *testing.T) *Bus {
	t.Helper()
	b := New()
	b.backoff = []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}
	t.Cleanup(func() { b.Close() })
	return b
}

func recv(t *testing.T, ch <-chan pubsub.Message) pubsub.Message {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(wait):
		t.Fatal("timed out waiting for a message")
		return pubsub.Message{}
	}
}

func collect(ch chan<- pubsub.Message) pubsub.Handler {
	return func(_ context.Context, m pubsub.Message) error {
		ch <- m
		return nil
	}
}

func TestEveryConsumerReceivesTheMessage(t *testing.T) {
	b := newBus(t)
	lib, subs := make(chan pubsub.Message, 1), make(chan pubsub.Message, 1)
	if err := b.Subscribe(t.Context(), "library", "t.v1", collect(lib)); err != nil {
		t.Fatal(err)
	}
	if err := b.Subscribe(t.Context(), "subtitles", "t.v1", collect(subs)); err != nil {
		t.Fatal(err)
	}

	msg := pubsub.Message{ID: "id-1", Topic: "t.v1", Payload: []byte("p")}
	if err := b.Publish(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []chan pubsub.Message{lib, subs} {
		got := recv(t, ch)
		if got.ID != "id-1" || got.Topic != "t.v1" || string(got.Payload) != "p" {
			t.Fatalf("got %+v", got)
		}
	}
}

func TestReplicasOfAConsumerShareTheLoad(t *testing.T) {
	b := newBus(t)
	var a, c atomic.Int32
	done := make(chan struct{}, 10)
	count := func(n *atomic.Int32) pubsub.Handler {
		return func(context.Context, pubsub.Message) error {
			n.Add(1)
			done <- struct{}{}
			return nil
		}
	}
	b.Subscribe(t.Context(), "library", "t.v1", count(&a))
	b.Subscribe(t.Context(), "library", "t.v1", count(&c))

	for range 4 {
		if err := b.Publish(t.Context(), pubsub.Message{Topic: "t.v1"}); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		select {
		case <-done:
		case <-time.After(wait):
			t.Fatal("timed out")
		}
	}
	if a.Load()+c.Load() != 4 || a.Load() != 2 {
		t.Fatalf("deliveries = %d and %d, want 2 and 2", a.Load(), c.Load())
	}
}

func TestPayloadIsCopiedPerDelivery(t *testing.T) {
	b := newBus(t)
	got := make(chan pubsub.Message, 1)
	b.Subscribe(t.Context(), "library", "t.v1", func(_ context.Context, m pubsub.Message) error {
		m.Payload[0] = 'x'
		got <- m
		return nil
	})
	payload := []byte("abc")
	b.Publish(t.Context(), pubsub.Message{Topic: "t.v1", Payload: payload})
	recv(t, got)
	if string(payload) != "abc" {
		t.Fatalf("publisher payload changed: %q", payload)
	}
}

func TestFailedHandlerIsRetried(t *testing.T) {
	b := newBus(t)
	var calls atomic.Int32
	done := make(chan struct{})
	b.Subscribe(t.Context(), "library", "t.v1", func(context.Context, pubsub.Message) error {
		if calls.Add(1) < 3 {
			return errors.New("temporary")
		}
		close(done)
		return nil
	})
	if err := b.Publish(t.Context(), pubsub.Message{Topic: "t.v1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(wait):
		t.Fatalf("timed out after %d calls", calls.Load())
	}
}

func TestGivesUpAfterTheBackoffs(t *testing.T) {
	b := newBus(t)
	var calls atomic.Int32
	b.Subscribe(t.Context(), "library", "t.v1", func(context.Context, pubsub.Message) error {
		calls.Add(1)
		return errors.New("permanent")
	})
	b.Publish(t.Context(), pubsub.Message{Topic: "t.v1"})

	// The first attempt plus one per backoff, then it stops.
	deadline := time.Now().Add(wait)
	for calls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 4 {
		t.Fatalf("calls = %d, want 4", calls.Load())
	}
}

func TestPublishDoesNotWaitForHandlers(t *testing.T) {
	b := newBus(t)
	release := make(chan struct{})
	b.Subscribe(t.Context(), "library", "t.v1", func(context.Context, pubsub.Message) error {
		<-release
		return nil
	})
	done := make(chan struct{})
	go func() {
		b.Publish(t.Context(), pubsub.Message{Topic: "t.v1"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
		t.Fatal("Publish blocked on the handler")
	}
	close(release)
}

func TestDeliveryOutlivesThePublishContext(t *testing.T) {
	b := newBus(t)
	got := make(chan error, 1)
	b.Subscribe(t.Context(), "library", "t.v1", func(ctx context.Context, _ pubsub.Message) error {
		got <- ctx.Err()
		return nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	b.Publish(ctx, pubsub.Message{Topic: "t.v1"})
	cancel()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("handler context: %v", err)
		}
	case <-time.After(wait):
		t.Fatal("timed out")
	}
}

func TestSubscriptionEndsWithItsContext(t *testing.T) {
	b := newBus(t)
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(t.Context())
	b.Subscribe(ctx, "library", "t.v1", func(context.Context, pubsub.Message) error {
		calls.Add(1)
		return nil
	})
	cancel()

	// AfterFunc runs asynchronously: wait until the consumer is gone.
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		n := len(b.topics["t.v1"])
		b.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	b.Publish(t.Context(), pubsub.Message{Topic: "t.v1"})
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("handler called %d times after the subscription ended", calls.Load())
	}
}

func TestSubscribeValidatesArguments(t *testing.T) {
	b := newBus(t)
	h := func(context.Context, pubsub.Message) error { return nil }
	for _, c := range []struct {
		consumer, topic string
		h               pubsub.Handler
	}{{"", "t", h}, {"c", "", h}, {"c", "t", nil}} {
		if err := b.Subscribe(t.Context(), c.consumer, c.topic, c.h); err == nil {
			t.Errorf("Subscribe(%q, %q, h=%v): want error", c.consumer, c.topic, c.h != nil)
		}
	}
}

func TestClose(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	wg.Add(1)
	b.Subscribe(t.Context(), "library", "t.v1", func(context.Context, pubsub.Message) error {
		defer wg.Done()
		return errors.New("fail, so it would be retried")
	})
	b.Publish(t.Context(), pubsub.Message{Topic: "t.v1"})
	wg.Wait()

	// Close must not wait for the default backoff of the retry.
	start := time.Now()
	b.Close()
	if time.Since(start) > time.Second {
		t.Fatal("Close waited for the retry")
	}
	if err := b.Publish(t.Context(), pubsub.Message{Topic: "t.v1"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Publish after Close: %v", err)
	}
	if err := b.Subscribe(t.Context(), "c", "t", func(context.Context, pubsub.Message) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe after Close: %v", err)
	}
}

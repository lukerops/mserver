// Package pubsub defines the message bus used to deliver events between
// modules. Implementations live in subpackages (memory in the monolith,
// nats in the distributed deployment).
//
// Events are published directly by the services, right after the commit of
// the change they describe: there is no outbox. A consumer that misses an
// event is healed by the Resync of the producer, so every handler must be
// idempotent.
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
)

// Message is an encoded event published on a topic.
type Message struct {
	// ID is the UUIDv7 of the event envelope. NATS JetStream uses it as the
	// Nats-Msg-Id header to drop duplicated publications.
	ID string
	// Topic is the versioned event type, e.g. "storage.file.indexed.v1".
	Topic string
	// Payload is the JSON encoding of the Envelope.
	Payload []byte
}

// Handler processes a message. Returning an error makes the bus deliver
// the message again later (Nak), so handlers must be idempotent.
type Handler func(ctx context.Context, msg Message) error

// Publisher publishes messages.
type Publisher interface {
	Publish(ctx context.Context, msg Message) error
}

// Subscriber registers handlers for a topic.
type Subscriber interface {
	// Subscribe registers h for the messages of topic on behalf of
	// consumer, the name of the consuming module (e.g. "library",
	// "subtitles"). Every consumer receives every message, but, among the
	// handlers subscribed with the same consumer name (the replicas of a
	// module), each message goes to only one. The subscription lasts until
	// ctx is cancelled.
	Subscribe(ctx context.Context, consumer, topic string, h Handler) error
}

// Encode returns the message that carries env.
func Encode[T any](env Envelope[T]) (Message, error) {
	payload, err := json.Marshal(env)
	if err != nil {
		return Message{}, fmt.Errorf("pubsub: encode %s: %w", env.Type, err)
	}
	return Message{ID: env.ID, Topic: env.Type, Payload: payload}, nil
}

// Decode returns the envelope carried by msg.
func Decode[T any](msg Message) (Envelope[T], error) {
	var env Envelope[T]
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return env, fmt.Errorf("pubsub: decode %s: %w", msg.Topic, err)
	}
	return env, nil
}

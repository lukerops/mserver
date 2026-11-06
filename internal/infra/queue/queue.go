// Package queue defines the message bus used to deliver events between
// modules. Implementations live in subpackages.
package queue

import "context"

// Message is an encoded event published on a topic.
type Message struct {
	Topic   string
	Payload []byte
}

// Handler processes a message. Returning an error makes the publisher
// deliver the message again later, so handlers must be idempotent.
type Handler func(ctx context.Context, msg Message) error

// Publisher publishes messages.
type Publisher interface {
	Publish(ctx context.Context, msg Message) error
}

// Subscriber registers handlers for a topic.
type Subscriber interface {
	Subscribe(topic string, h Handler)
}

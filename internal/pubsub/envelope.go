package pubsub

import (
	"time"

	"github.com/google/uuid"
)

// SpecVersion is the CloudEvents version implemented by Envelope.
const SpecVersion = "1.0"

// Envelope is an event in the CloudEvents 1.0 JSON format. Data holds the
// event-specific payload, defined in the events package of the module that
// publishes it.
type Envelope[T any] struct {
	// ID is a UUIDv7, used for ordering and deduplication.
	ID string `json:"id"`
	// Type is the versioned event name, e.g. "storage.file.indexed.v1".
	// It is also the topic the event is published on.
	Type string `json:"type"`
	// Source is the module that produced the event, e.g. "storage".
	Source      string    `json:"source"`
	SpecVersion string    `json:"specversion"`
	Time        time.Time `json:"time"`
	// Subject is the main resource of the event, e.g. a FileID.
	Subject string `json:"subject,omitempty"`
	Data    T      `json:"data"`
}

// NewEnvelope wraps data in a new event.
func NewEnvelope[T any](typ, source, subject string, data T) Envelope[T] {
	return Envelope[T]{
		ID:          uuid.Must(uuid.NewV7()).String(),
		Type:        typ,
		Source:      source,
		SpecVersion: SpecVersion,
		Time:        time.Now().UTC(),
		Subject:     subject,
		Data:        data,
	}
}

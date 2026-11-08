// Package events is the event API of the storage module: the types and
// the data of the events it publishes. Like the DTOs of the router
// package, every type carries the version of the event.
//
// Events are wrapped in queue.Envelope and published on the topic named
// after their type. Delivery is at-least-once: consumers must tolerate
// duplicates.
package events

// Source is the CloudEvents source of the storage events.
const Source = "storage"

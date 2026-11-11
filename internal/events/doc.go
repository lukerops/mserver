// Package events is the event API of mserver: the types and the data of
// every event published by the modules, in one place. Each context has its
// own files (e.g. storage_files.go, storage_ffprobe.go). Every type carries
// the version of the event.
//
// Events are wrapped in pubsub.Envelope and published on the topic named
// after their type. Delivery is at-least-once: consumers must tolerate
// duplicates.
package events

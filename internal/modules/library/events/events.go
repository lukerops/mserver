// Package events is the event API of the library module: the types and
// the data of the events it publishes. Like the DTOs of the router
// package, every type carries the version of the event.
//
// Events are wrapped in queue.Envelope and published on the topic named
// after their type. Delivery is at-least-once: consumers must tolerate
// duplicates.
package events

import "github.com/google/uuid"

// Source is the CloudEvents source of the library events.
const Source = "library"

// Types of the movie events. The subject is the movie id.
const (
	// MovieAddedV1 is a new movie (or a movie republished by a resync).
	// Its data is MovieEventV1.
	MovieAddedV1 = "library.movie.added.v1"
	// MovieUpdatedV1 is a changed movie (e.g. renamed or analyzed). Its
	// data is MovieEventV1.
	MovieUpdatedV1 = "library.movie.updated.v1"
	// MovieRemovedV1 is a movie whose file or library was removed. Its
	// data is MovieRemovedEventV1.
	MovieRemovedV1 = "library.movie.removed.v1"
)

// MovieEventV1 is the data of the movie added and updated events.
type MovieEventV1 struct {
	MovieID    uuid.UUID `json:"movie_id"`
	LibraryID  uuid.UUID `json:"library_id"`
	FileID     uuid.UUID `json:"file_id"`
	LocationID uuid.UUID `json:"location_id"`
	// Path is relative to the location.
	Path  string `json:"path"`
	Title string `json:"title"`
	// Year is omitted when unknown.
	Year int `json:"year,omitempty"`
	// DurationMs is omitted until the file is analyzed.
	DurationMs int64 `json:"duration_ms,omitempty"`
}

// MovieRemovedEventV1 is the data of the movie removed event.
type MovieRemovedEventV1 struct {
	MovieID   uuid.UUID `json:"movie_id"`
	LibraryID uuid.UUID `json:"library_id"`
	FileID    uuid.UUID `json:"file_id"`
}

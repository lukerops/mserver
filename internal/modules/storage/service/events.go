package service

import (
	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
)

// Source is the CloudEvents source of the storage events.
const Source = "storage"

// Versioned types of the storage events. Events are published on a topic
// with the same name. Delivery is at-least-once: consumers must tolerate
// duplicates.
const (
	EventFileIndexed = "storage.file.indexed.v1"
	EventFileChanged = "storage.file.changed.v1"
	EventFileMoved   = "storage.file.moved.v1"
	EventFileRemoved = "storage.file.removed.v1"
)

// FileEvent is the data of every file event. The subject is the file id.
type FileEvent struct {
	FileID     uuid.UUID `json:"file_id"`
	LocationID uuid.UUID `json:"location_id"`
	// Path is relative to the location. For removed files it is the last
	// known path.
	Path string `json:"path"`
}

func fileEvent(typ string, location, id uuid.UUID, path string) queue.Envelope[FileEvent] {
	return queue.NewEnvelope(typ, Source, id.String(), FileEvent{
		FileID:     id,
		LocationID: location,
		Path:       path,
	})
}

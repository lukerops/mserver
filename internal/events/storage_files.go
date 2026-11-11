package events

import (
	"encoding/json"

	"github.com/google/uuid"
)

// SourceStorage is the CloudEvents source of the storage events.
const SourceStorage = "storage"

// Types of the file events. The subject is the file id.
const (
	// FileIndexedV1 is a new file (or a file republished by a resync).
	// Its data is FileEventV1.
	FileIndexedV1 = "storage.file.indexed.v1"
	// FileChangedV1 is a file whose content changed at the same path.
	// Its data is FileEventV1.
	FileChangedV1 = "storage.file.changed.v1"
	// FileMovedV1 is a file renamed or moved, keeping its id. Its data is
	// FileEventV1.
	FileMovedV1 = "storage.file.moved.v1"
	// FileRemovedV1 is a file that no longer exists. Its data is
	// FileEventV1.
	FileRemovedV1 = "storage.file.removed.v1"
	// FileAnalyzedV1 is the result of an analyzer on the current content
	// of a file. Its data is FileAnalyzedEventV1.
	FileAnalyzedV1 = "storage.file.analyzed.v1"
)

// FileEventV1 is the data of the indexed, changed, moved and removed
// events.
type FileEventV1 struct {
	FileID     uuid.UUID `json:"file_id"`
	LocationID uuid.UUID `json:"location_id"`
	// Path is relative to the location. For removed files it is the last
	// known path.
	Path string `json:"path"`
}

// FileAnalyzedEventV1 is the data of the analyzed event.
type FileAnalyzedEventV1 struct {
	FileID     uuid.UUID `json:"file_id"`
	LocationID uuid.UUID `json:"location_id"`
	// Analyzer is the name of the analyzer (e.g. AnalyzerFFprobe).
	Analyzer string `json:"analyzer"`
	// Version is the version of the result format of the analyzer.
	Version int `json:"version"`
	// Result depends on the analyzer and its version (e.g.
	// FFprobeResultV1 for AnalyzerFFprobe version 1).
	Result json.RawMessage `json:"result"`
}

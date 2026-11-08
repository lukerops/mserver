// Package analyzer defines the contract of the storage file analyzers:
// plugins (plugins/storage/<name>) that inspect indexed files and produce
// a result published in the storage.file.analyzed.v1 event.
//
// Analyzers run only inside the storage process, so, unlike the REST
// contracts, they receive the local path of the file.
package analyzer

import (
	"context"
	"errors"
)

// ErrInvalidFile is returned by Analyze when the file cannot be analyzed
// (e.g. it is corrupted). The file is analyzed again only when it changes.
// Any other error is retried later.
var ErrInvalidFile = errors.New("invalid file")

// Analyzer inspects files.
type Analyzer interface {
	// Name identifies the analyzer and its results (e.g. "ffprobe").
	Name() string
	// Version is the version of the result. Raising it makes every file
	// be analyzed again.
	Version() int
	// Accepts reports whether the analyzer inspects the file at path
	// (slash-separated and relative to its location).
	Accepts(path string) bool
	// Analyze inspects the file at localPath. The result is encoded as
	// JSON in the storage.file.analyzed.v1 event.
	Analyze(ctx context.Context, localPath string) (any, error)
}

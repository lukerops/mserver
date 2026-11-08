// Package store defines the persistence of the storage index: its records
// and the Store interface. Database backends implement Store in
// subpackages (e.g. store/sqlite).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errors returned by Store implementations, wrapped with context.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// Store persists locations and files.
type Store interface {
	// WithTx runs fn in a transaction, committing if fn succeeds. Store
	// methods called with the context given to fn join the transaction.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error

	// InsertLocation saves a new location. It returns ErrAlreadyExists if
	// root is inside, contains or equals the root of another location.
	InsertLocation(ctx context.Context, loc Location, root string) error
	// GetLocation returns ErrNotFound if the location does not exist.
	GetLocation(ctx context.Context, id uuid.UUID) (Location, error)
	// ListLocations returns every location ordered by ID.
	ListLocations(ctx context.Context) ([]Location, error)
	// DeleteLocation removes a location and its files. It returns
	// ErrNotFound if the location does not exist.
	DeleteLocation(ctx context.Context, id uuid.UUID) error

	// GetFile returns ErrNotFound if the file does not exist.
	GetFile(ctx context.Context, id uuid.UUID) (File, error)
	// ListFiles returns the files of a location ordered by path.
	ListFiles(ctx context.Context, location uuid.UUID) ([]File, error)
	// Siblings returns the other files in the same directory as f,
	// ordered by path.
	Siblings(ctx context.Context, f File) ([]Sibling, error)

	// ApplyScan applies the changes of a scan atomically.
	ApplyScan(ctx context.Context, location uuid.UUID, c ScanChanges) error

	// PendingAnalysis returns the files modified at or before
	// stableBefore that have no analysis of the analyzer at this version
	// for their current size and mtime, ordered by ID.
	PendingAnalysis(ctx context.Context, analyzer string, version int, stableBefore time.Time) ([]File, error)
	// SaveAnalysis saves the analysis of a file, replacing the previous
	// one of the same analyzer.
	SaveAnalysis(ctx context.Context, a Analysis) error
	// ListAnalyses returns the successful analyses of the files of a
	// location that match the current state of the files, ordered by
	// file ID.
	ListAnalyses(ctx context.Context, location uuid.UUID) ([]Analysis, error)
}

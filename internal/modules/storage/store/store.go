// Package store defines the persistence of the storage index. Database
// backends implement Store in subpackages (e.g. store/sqlite).
package store

import (
	"context"

	"github.com/lukerops/mserver/internal/modules/storage/api"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

// Store persists locations and files.
type Store interface {
	// WithTx runs fn in a transaction, committing if fn succeeds. Store
	// methods called with the context given to fn join the transaction.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error

	// InsertLocation saves a new location. It returns api.ErrAlreadyExists
	// if root is inside, contains or equals the root of another location.
	InsertLocation(ctx context.Context, loc api.Location, root string) error
	// GetLocation returns api.ErrNotFound if the location does not exist.
	GetLocation(ctx context.Context, id api.LocationID) (api.Location, error)
	ListLocations(ctx context.Context) ([]api.Location, error)
	// DeleteLocation removes a location and its files. It returns
	// api.ErrNotFound if the location does not exist.
	DeleteLocation(ctx context.Context, id api.LocationID) error

	// GetFile returns api.ErrNotFound if the file does not exist.
	GetFile(ctx context.Context, id api.FileID) (api.File, error)
	// ListFiles returns the files of a location ordered by path.
	ListFiles(ctx context.Context, id api.LocationID) ([]api.File, error)
	// Siblings returns the other files in the same directory as f,
	// ordered by path.
	Siblings(ctx context.Context, f api.File) ([]api.Sibling, error)

	// ApplyScan applies the changes of a scan atomically.
	ApplyScan(ctx context.Context, id api.LocationID, c ScanChanges) error
}

// ScanChanges is the difference between the index and a location walk.
type ScanChanges struct {
	Added   map[api.FileID]driver.FileInfo
	Updated map[api.FileID]driver.FileInfo // changed or moved
	Removed []api.FileID
}

// Package api is the public contract of the storage module. Other modules
// depend only on this package.
package api

import "context"

// Service is the storage contract. Files are referred to by FileID; paths
// only appear relative to a location.
type Service interface {
	// CreateLocation registers a location. It fails if the directory is
	// inside, or contains, another location.
	CreateLocation(ctx context.Context, req CreateLocationRequest) (Location, error)
	GetLocation(ctx context.Context, id LocationID) (Location, error)
	ListLocations(ctx context.Context) ([]Location, error)
	// DeleteLocation removes a location and its files from the index.
	DeleteLocation(ctx context.Context, id LocationID) error
	// ScanLocation synchronizes the index with the files of the location.
	// If the location is not accessible, the index is left untouched.
	ScanLocation(ctx context.Context, id LocationID) (ScanResult, error)

	GetFile(ctx context.Context, id FileID) (File, error)
	// ListFiles lists the files of a location ordered by path.
	ListFiles(ctx context.Context, id LocationID) ([]File, error)
	// ListSiblings lists the other indexed files in the same directory
	// (e.g. external subtitles).
	ListSiblings(ctx context.Context, id FileID) ([]Sibling, error)
	// Resolve returns a "file://" URL to read the file from.
	Resolve(ctx context.Context, id FileID) (string, error)
}

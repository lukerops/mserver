package service

import "github.com/lukerops/mserver/internal/modules/storage/store"

// The records are the ones of the store.
type (
	Location = store.Location
	File     = store.File
	Sibling  = store.Sibling
	Analysis = store.Analysis
)

// CreateLocationRequest registers a location.
type CreateLocationRequest struct {
	Driver string
	// Options are driver specific (e.g. "dir" for the local driver).
	Options map[string]string
}

// ScanResult summarizes a location scan.
type ScanResult struct {
	Added   int
	Changed int
	Moved   int
	Removed int
}

package api

import "time"

// Location is a directory registered by the operator and accessed through
// a driver. Locations never overlap, so every file belongs to exactly one
// location.
type Location struct {
	ID     LocationID
	Driver string
	// Options are driver specific (e.g. "dir" for the local driver).
	Options   map[string]string
	CreatedAt time.Time
}

// CreateLocationRequest registers a location.
type CreateLocationRequest struct {
	Driver  string
	Options map[string]string
}

// ScanResult summarizes a location scan.
type ScanResult struct {
	Added   int
	Changed int
	Moved   int
	Removed int
}

// Package driver defines the backend interface used by the storage module
// to access a location. Drivers are backends chosen by the operator and
// never leave the storage module.
//
// Paths are always slash-separated and relative to the location root.
package driver

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

// ErrInvalidOptions is returned by factories for invalid location
// options, wrapped with context.
var ErrInvalidOptions = errors.New("invalid options")

// FileInfo describes a regular file.
type FileInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
	// Inode is zero when the driver does not support inodes.
	Inode uint64
}

// File is an open regular file.
type File interface {
	io.ReadSeekCloser
	Stat() (fs.FileInfo, error)
}

// Driver gives access to the files of one location.
type Driver interface {
	// Root identifies the location directory (e.g. "local:/mnt/media").
	// Roots are slash-separated so overlapping locations can be detected.
	Root() string
	// Check verifies that the location root is accessible.
	Check(ctx context.Context) error
	// Walk visits every regular file. It fails if any directory cannot be
	// read, so a partial walk is never mistaken for removed files.
	Walk(ctx context.Context, fn func(FileInfo) error) error
	// LocalPath returns the path of a file on the local filesystem.
	LocalPath(path string) string
	// Open opens a regular file for reading. A missing file (or one that
	// is not regular) returns an error matching fs.ErrNotExist.
	Open(ctx context.Context, path string) (File, error)
}

// Factory creates a driver from location options. Invalid options return
// ErrInvalidOptions.
type Factory func(options map[string]string) (Driver, error)

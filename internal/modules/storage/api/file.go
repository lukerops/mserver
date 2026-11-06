package api

import "time"

// File is an indexed file.
type File struct {
	ID         FileID
	LocationID LocationID
	// Path is slash-separated and relative to the location.
	Path    string
	Size    int64
	ModTime time.Time
	// Inode is zero when the driver does not support inodes.
	Inode uint64
}

// Sibling is a file in the same directory as another one.
type Sibling struct {
	FileID FileID
	// Path is relative to the location.
	Path string
}

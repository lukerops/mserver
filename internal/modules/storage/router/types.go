package router

import (
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/service"
)

// ErrorV1 is the body of every error response.
type ErrorV1 struct {
	Error string `json:"error"`
}

type LocationV1 struct {
	ID     uuid.UUID `json:"id"`
	Driver string    `json:"driver"`
	// Options are driver specific (e.g. "dir" for the local driver).
	Options   map[string]string `json:"options"`
	CreatedAt time.Time         `json:"created_at"`
}

type LocationListV1 struct {
	Locations []LocationV1 `json:"locations"`
}

type CreateLocationRequestV1 struct {
	Driver  string            `json:"driver"`
	Options map[string]string `json:"options"`
}

// ScanResultV1 summarizes a location scan.
type ScanResultV1 struct {
	Added   int `json:"added"`
	Changed int `json:"changed"`
	Moved   int `json:"moved"`
	Removed int `json:"removed"`
}

type FileV1 struct {
	ID         uuid.UUID `json:"id"`
	LocationID uuid.UUID `json:"location_id"`
	// Path is slash-separated and relative to the location.
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	// Inode is zero when the driver does not support inodes.
	Inode uint64 `json:"inode"`
}

type FileListV1 struct {
	Files []FileV1 `json:"files"`
}

// SiblingV1 is a file in the same directory as another one.
type SiblingV1 struct {
	FileID uuid.UUID `json:"file_id"`
	// Path is relative to the location.
	Path string `json:"path"`
}

type SiblingListV1 struct {
	Siblings []SiblingV1 `json:"siblings"`
}

// ResolveResponseV1 holds the URL to read the content of a file from:
// "http://<storage address>/locations/{location_id}/files/{file_id}",
// which accepts GET and HEAD with Range.
type ResolveResponseV1 struct {
	URL string `json:"url"`
}

func toLocationV1(l service.Location) LocationV1 {
	options := l.Options
	if options == nil {
		options = map[string]string{}
	}
	return LocationV1{
		ID:        l.ID,
		Driver:    l.Driver,
		Options:   options,
		CreatedAt: l.CreatedAt,
	}
}

func toFileV1(f service.File) FileV1 {
	return FileV1{
		ID:         f.ID,
		LocationID: f.LocationID,
		Path:       f.Path,
		Size:       f.Size,
		ModTime:    f.ModTime,
		Inode:      f.Inode,
	}
}

package storage

import "io"

type FileID string

type StorageService interface {
	Save(storage string, path string, src io.Reader) (FileID, error)
	GetReader(id FileID) (io.ReadSeekCloser, error)
	Delete(id FileID) error
	Move(id FileID, path string) error
	// GetInfo(id FileID) (FileInfo, error)
}

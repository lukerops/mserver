package local

import (
	"fmt"
	"io"
	"os"

	"github.com/lukerops/mserver/internal/modules/storage"
)

type localDriver struct {
	basePath string
}

func NewLocalBackend(basePath string) storage.StorageBackend {
	return &localDriver{
		basePath: basePath,
	}
}

func (l *localDriver) Save(path string, src io.Reader) (string, error) {
	return "", nil
}

func (l *localDriver) GetReader(internalKey string) (io.ReadSeekCloser, error) {
	return os.Open(internalKey)
}

func (l *localDriver) Delete(internalKey string) error {
	return os.Remove(internalKey)
}

func (l *localDriver) Move(internalKey string, path string) (string, error) {
	newInternalKey := fmt.Sprintf("%s/%s", l.basePath, path)
	if err := os.Rename(internalKey, newInternalKey); err != nil {
		return "", err
	}
	return newInternalKey, nil
}

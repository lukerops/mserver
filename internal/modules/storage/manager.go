package storage

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
)

type DriverType string

const (
	DriverLocal DriverType = "local"
)

type StorageConfig struct {
	Name     string
	Driver   DriverType
	BasePath string
}

type StorageBackend interface {
	Save(path string, src io.Reader) (string, error)
	GetReader(internalKey string) (io.ReadSeekCloser, error)
	Delete(internalKey string) error
	Move(internalKey string, path string) (string, error)
}

type StorageManager struct {
	backends map[string]StorageBackend
}

func (m *StorageManager) RegisterBackend(config StorageConfig) error {
	switch config.Driver {
	case DriverLocal:
		// m.backends[config.Name] = local.NewLocalBackend(config.BasePath)
	default:
		return fmt.Errorf("driver desconhecido: %s", config.Driver)
	}
	return nil
}

func (m *StorageManager) Save(storage string, path string, src io.Reader) (FileID, error) {
	backend, exists := m.backends[storage]
	if !exists {
		return "", fmt.Errorf("storage desconhecido: %s", storage)
	}

	internalKey, err := backend.Save(path, src)
	if err != nil {
		return "", err
	}

	return m.saveMetadata(storage, internalKey)
}

func (m *StorageManager) GetReader(id FileID) (io.ReadSeekCloser, error) {
	backendName, internalKey := m.resolveMetadata(id)
	backend := m.backends[backendName]
	return backend.GetReader(internalKey)
}

func (m *StorageManager) Delete(id FileID) error {
	backendName, internalKey := m.resolveMetadata(id)
	backend := m.backends[backendName]
	if err := backend.Delete(internalKey); err != nil {
		return err
	}

	// Remove do banco de dados
	return nil
}

func (m *StorageManager) Move(id FileID, path string) error {
	backendName, internalKey := m.resolveMetadata(id)
	backend := m.backends[backendName]
	newInternalKey, err := backend.Move(internalKey, path)
	if err != nil {
		return err
	}

	if internalKey == newInternalKey {
		return nil
	}

	// Atualiza a internal key no banco de dados
	internalKey = newInternalKey
	return nil
}

func (m *StorageManager) hash(backendName string, internalKey string) (string, error) {
	backend := m.backends[backendName]
	file, err := backend.GetReader(internalKey)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Cria um buffer de 2MB na RAM para ler o disco em blocos maiores.
	// Isso reduz o estresse de I/O em arquivos de 50GB-100GB.
	tamanhoBuffer := 2 * 1024 * 1024 // 2 MiB
	leitorComBuffer := bufio.NewReaderSize(file, tamanhoBuffer)

	hasher := sha256.New()

	// io.Copy vai ler do leitor com buffer e alimentar o hasher
	if _, err := io.Copy(hasher, leitorComBuffer); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func (m *StorageManager) resolveMetadata(id FileID) (string, string) {
	// Busca no banco de dados
	backendName, internalKey := "movies_hdd", "xyz"
	return backendName, internalKey
}

func (m *StorageManager) saveMetadata(backendName string, internalKey string) (FileID, error) {
	hash, err := m.hash(backendName, internalKey)
	if err != nil {
		return "", err
	}

	// Salva no banco de dados
	return FileID(hash), nil
}

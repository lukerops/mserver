package store

import "errors"

// Errors returned by the Store, wrapped with context: use errors.Is.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

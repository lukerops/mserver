// Package kv defines a key-value store for short-lived state that must be
// shared by every instance of a module (e.g. playback sessions). Values
// are opaque bytes and keys expire after their TTL.
//
// Each operation maps to a single native command of Valkey/Redis (GET,
// SET NX PX, SET XX PX, DEL, SCAN + GET), so that a networked
// implementation needs no scripts. There are no transactions: when two
// writers race on the same key, the last write wins.
//
// Implementations live in subpackages.
package kv

import (
	"context"
	"errors"
	"time"
)

// Errors of the stores. Use errors.Is to check them.
var (
	// ErrNotFound is returned for a missing (or expired) key.
	ErrNotFound = errors.New("kv: not found")
	// ErrExists is returned by Create when the key already exists.
	ErrExists = errors.New("kv: already exists")
)

// Entry is a key and its value.
type Entry struct {
	Key   string
	Value []byte
}

// Store is safe for concurrent use. A TTL of zero or less means the key
// never expires.
type Store interface {
	// Get returns the value of a key, or ErrNotFound.
	Get(ctx context.Context, key string) ([]byte, error)
	// Create sets a key that does not exist (SET NX PX), or returns
	// ErrExists.
	Create(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Update replaces the value and the TTL of an existing key (SET XX
	// PX), or returns ErrNotFound.
	Update(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes a key (DEL), or returns ErrNotFound if there was
	// none: of concurrent deletes, exactly one succeeds.
	Delete(ctx context.Context, key string) error
	// List returns the entries whose key starts with prefix, in no
	// particular order (SCAN MATCH + GET). Keys removed during the listing
	// may be missing.
	List(ctx context.Context, prefix string) ([]Entry, error)
}

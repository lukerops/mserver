// Package memory implements kv.Store in the memory of the process. It
// is enough while the module runs in a single instance; expired keys are
// removed when they are next touched.
package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/lukerops/mserver/internal/infra/kv"
)

type item struct {
	value []byte
	// expires is zero when the key never expires.
	expires time.Time
}

// Store is an in-memory kv.Store.
type Store struct {
	mu    sync.Mutex
	items map[string]item
	now   func() time.Time
}

var _ kv.Store = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{items: make(map[string]item), now: time.Now}
}

// get returns the live item of a key, removing it if it expired. The
// caller holds mu.
func (s *Store) get(key string) (item, bool) {
	it, ok := s.items[key]
	if !ok {
		return item{}, false
	}
	if !it.expires.IsZero() && !s.now().Before(it.expires) {
		delete(s.items, key)
		return item{}, false
	}
	return it, true
}

func (s *Store) set(key string, value []byte, ttl time.Duration) {
	it := item{value: clone(value)}
	if ttl > 0 {
		it.expires = s.now().Add(ttl)
	}
	s.items[key] = it
}

func (s *Store) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.get(key)
	if !ok {
		return nil, kv.ErrNotFound
	}
	return clone(it.value), nil
}

func (s *Store) Create(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.get(key); ok {
		return kv.ErrExists
	}
	s.set(key, value, ttl)
	return nil
}

func (s *Store) Update(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.get(key); !ok {
		return kv.ErrNotFound
	}
	s.set(key, value, ttl)
	return nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.get(key); !ok {
		return kv.ErrNotFound
	}
	delete(s.items, key)
	return nil
}

func (s *Store) List(_ context.Context, prefix string) ([]kv.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []kv.Entry
	for key := range s.items {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if it, ok := s.get(key); ok {
			out = append(out, kv.Entry{Key: key, Value: clone(it.value)})
		}
	}
	return out, nil
}

func clone(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return append([]byte(nil), b...)
}

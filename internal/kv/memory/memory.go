// Package memory implements kv.Store in the memory of the process. It
// is enough while the module runs in a single instance.
//
// Expired keys are never returned, and are removed when they are next
// touched and by a background sweep, so that keys nobody reads again do not
// pile up. Call Close to stop the sweep.
package memory

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lukerops/mserver/internal/kv"
)

// DefaultSweepInterval is how often the expired keys are removed, unless
// WithSweepInterval says otherwise.
const DefaultSweepInterval = time.Minute

type item struct {
	value []byte
	// expires is zero when the key never expires.
	expires time.Time
}

var _ kv.Store = (*Store)(nil)

// Store is an in-memory kv.Store.
type Store struct {
	mu    sync.Mutex
	items map[string]item
	now   func() time.Time

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// Option configures a Store.
type Option func(*options)

type options struct {
	sweepInterval time.Duration
}

// WithSweepInterval sets how often the expired keys are removed. The
// default is DefaultSweepInterval; zero or less disables the sweep (expired
// keys are then only removed when touched).
func WithSweepInterval(d time.Duration) Option {
	return func(o *options) { o.sweepInterval = d }
}

// New returns an empty store and starts its sweep. Close it when done.
func New(opts ...Option) *Store {
	o := options{sweepInterval: DefaultSweepInterval}
	for _, opt := range opts {
		opt(&o)
	}

	s := &Store{
		items: make(map[string]item),
		now:   time.Now,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	if o.sweepInterval > 0 {
		go s.sweepLoop(o.sweepInterval)
	} else {
		close(s.done)
	}
	return s
}

// Close stops the sweep. The store keeps working, without it. It is safe to
// call more than once.
func (s *Store) Close() error {
	s.closeOnce.Do(func() { close(s.stop) })
	<-s.done
	return nil
}

func (s *Store) sweepLoop(interval time.Duration) {
	defer close(s.done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.sweep()
		case <-s.stop:
			return
		}
	}
}

// sweep removes every expired key.
func (s *Store) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for key, it := range s.items {
		if !it.expires.IsZero() && !now.Before(it.expires) {
			delete(s.items, key)
		}
	}
}

// get returns the live item of a key, removing it if it expired. The
// caller holds mu.
func (s *Store) get(key string) (*item, bool) {
	it, ok := s.items[key]
	if !ok {
		return nil, false
	}
	if !it.expires.IsZero() && !s.now().Before(it.expires) {
		delete(s.items, key)
		return nil, false
	}
	return &it, true
}

func (s *Store) set(key string, value []byte, ttl time.Duration) {
	it := item{value: slices.Clone(value)}
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

	return slices.Clone(it.value), nil
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
			out = append(out, kv.Entry{
				Key:   key,
				Value: slices.Clone(it.value),
			})
		}
	}
	return out, nil
}

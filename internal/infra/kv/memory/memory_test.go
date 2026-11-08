package memory

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lukerops/mserver/internal/infra/kv"
)

func TestOperations(t *testing.T) {
	ctx := t.Context()
	s := New()

	if _, err := s.Get(ctx, "a"); !errors.Is(err, kv.ErrNotFound) {
		t.Fatalf("Get of missing key: %v", err)
	}
	if err := s.Update(ctx, "a", []byte("1"), 0); !errors.Is(err, kv.ErrNotFound) {
		t.Fatalf("Update of missing key: %v", err)
	}
	if err := s.Create(ctx, "a", []byte("1"), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, "a", []byte("2"), 0); !errors.Is(err, kv.ErrExists) {
		t.Fatalf("second Create: %v", err)
	}
	if err := s.Update(ctx, "a", []byte("3"), 0); err != nil {
		t.Fatal(err)
	}
	v, err := s.Get(ctx, "a")
	if err != nil || string(v) != "3" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	// The returned value is a copy.
	v[0] = 'x'
	if v, _ := s.Get(ctx, "a"); string(v) != "3" {
		t.Fatalf("value changed through Get: %q", v)
	}

	if err := s.Create(ctx, "ab", []byte("4"), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, "b", []byte("5"), 0); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(entries, func(a, b kv.Entry) int { return strings.Compare(a.Key, b.Key) })
	if len(entries) != 2 || entries[0].Key != "a" || entries[1].Key != "ab" || string(entries[1].Value) != "4" {
		t.Fatalf("List = %+v", entries)
	}

	if err := s.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "a"); !errors.Is(err, kv.ErrNotFound) {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestExpiration(t *testing.T) {
	ctx := t.Context()
	s := New()
	now := time.Now()
	s.now = func() time.Time { return now }

	if err := s.Create(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, "forever", []byte("v"), 0); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	// Update renews the TTL.
	if err := s.Update(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	now = now.Add(45 * time.Second)
	if _, err := s.Get(ctx, "k"); err != nil {
		t.Fatalf("Get before expiration: %v", err)
	}
	now = now.Add(15 * time.Second)
	if _, err := s.Get(ctx, "k"); !errors.Is(err, kv.ErrNotFound) {
		t.Fatalf("Get after expiration: %v", err)
	}
	if err := s.Delete(ctx, "k"); !errors.Is(err, kv.ErrNotFound) {
		t.Fatalf("Delete after expiration: %v", err)
	}
	// An expired key can be created again.
	if err := s.Create(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Create after expiration: %v", err)
	}
	now = now.Add(time.Hour)
	if entries, _ := s.List(ctx, ""); len(entries) != 1 || entries[0].Key != "forever" {
		t.Fatalf("List = %+v", entries)
	}
}

func TestConcurrentDeleteHasOneWinner(t *testing.T) {
	ctx := t.Context()
	s := New()
	if err := s.Create(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if s.Delete(ctx, "k") == nil {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d deletes succeeded, want 1", wins.Load())
	}
}

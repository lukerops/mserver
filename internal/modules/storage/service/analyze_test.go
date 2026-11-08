package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/events"
)

// fakeAnalyzer accepts .mkv files. Files whose content is "bad" are
// invalid; when broken is set every analysis fails as if the analyzer were
// not installed.
type fakeAnalyzer struct {
	mu     sync.Mutex
	calls  map[string]int
	broken bool
}

type fakeResult struct {
	Content string `json:"content"`
}

func (a *fakeAnalyzer) Name() string { return "fake" }
func (a *fakeAnalyzer) Version() int { return 1 }
func (a *fakeAnalyzer) Accepts(path string) bool {
	return strings.HasSuffix(path, ".mkv")
}

func (a *fakeAnalyzer) Analyze(_ context.Context, path string) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.broken {
		return nil, errors.New("executable not found")
	}
	if a.calls == nil {
		a.calls = make(map[string]int)
	}
	a.calls[filepath.Base(path)]++
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if string(b) == "bad" {
		return nil, analyzer.ErrInvalidFile
	}
	return fakeResult{Content: string(b)}, nil
}

func (a *fakeAnalyzer) callsOf(name string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[name]
}

// writeStableFile writes a file modified long enough ago to be analyzed.
func writeStableFile(t *testing.T, path, content string, age time.Duration) {
	t.Helper()
	writeFile(t, path, content)
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func analyzePending(t *testing.T, s Service) int {
	t.Helper()
	n, err := s.AnalyzePending(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// subscribe collects the payloads published on topic.
func subscribe(bus *memory.Bus, topic string) <-chan []byte {
	got := make(chan []byte, 100)
	bus.Subscribe(topic, func(_ context.Context, msg queue.Message) error {
		got <- msg.Payload
		return nil
	})
	return got
}

func nextAnalyzed(t *testing.T, got <-chan []byte) queue.Envelope[events.FileAnalyzedEventV1] {
	t.Helper()
	select {
	case p := <-got:
		var e queue.Envelope[events.FileAnalyzedEventV1]
		if err := json.Unmarshal(p, &e); err != nil {
			t.Fatal(err)
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no analyzed event")
		return queue.Envelope[events.FileAnalyzedEventV1]{}
	}
}

func noMoreEvents(t *testing.T, got <-chan []byte) {
	t.Helper()
	select {
	case p := <-got:
		t.Fatalf("unexpected event %s", p)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAnalyzePending(t *testing.T) {
	bus := memory.New()
	analyzed := subscribe(bus, events.FileAnalyzedV1)
	a := &fakeAnalyzer{}
	s := newTestService(t, t.TempDir(), bus, a)

	dir := t.TempDir()
	writeStableFile(t, filepath.Join(dir, "good.mkv"), "good", time.Hour)
	writeStableFile(t, filepath.Join(dir, "bad.mkv"), "bad", time.Hour)
	writeStableFile(t, filepath.Join(dir, "notes.txt"), "notes", time.Hour)
	writeFile(t, filepath.Join(dir, "copying.mkv"), "copying") // not stable yet
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)
	ids := files(t, s, loc.ID)

	if n := analyzePending(t, s); n != 2 {
		t.Fatalf("first round analyzed %d files, want 2", n)
	}
	e := nextAnalyzed(t, analyzed)
	var result fakeResult
	if err := json.Unmarshal(e.Data.Result, &result); err != nil {
		t.Fatal(err)
	}
	if e.Data.FileID != ids["good.mkv"] || e.Data.LocationID != loc.ID ||
		e.Data.Analyzer != "fake" || e.Data.Version != 1 || result.Content != "good" ||
		e.Subject != ids["good.mkv"].String() {
		t.Fatalf("analyzed event = %+v (result %s)", e, e.Data.Result)
	}
	noMoreEvents(t, analyzed) // invalid files publish nothing

	// Nothing changed: neither the valid nor the invalid file is retried.
	if n := analyzePending(t, s); n != 0 {
		t.Fatalf("second round analyzed %d files, want 0", n)
	}
	if a.callsOf("bad.mkv") != 1 || a.callsOf("copying.mkv") != 0 {
		t.Fatalf("calls = %v", a.calls)
	}

	// A changed file is analyzed again.
	writeStableFile(t, filepath.Join(dir, "good.mkv"), "better", 30*time.Minute)
	scan(t, s, loc.ID)
	if n := analyzePending(t, s); n != 1 {
		t.Fatalf("round after change analyzed %d files, want 1", n)
	}
	e = nextAnalyzed(t, analyzed)
	if err := json.Unmarshal(e.Data.Result, &result); err != nil || result.Content != "better" {
		t.Fatalf("result after change = %s (%v)", e.Data.Result, err)
	}
}

func TestAnalyzerFailureIsRetried(t *testing.T) {
	a := &fakeAnalyzer{broken: true}
	s := newTestService(t, t.TempDir(), memory.New(), a)
	dir := t.TempDir()
	writeStableFile(t, filepath.Join(dir, "movie.mkv"), "movie", time.Hour)
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)

	if _, err := s.AnalyzePending(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("AnalyzePending: got %v, want ErrUnavailable", err)
	}

	a.mu.Lock()
	a.broken = false
	a.mu.Unlock()
	if n := analyzePending(t, s); n != 1 {
		t.Fatalf("retry analyzed %d files, want 1", n)
	}
}

func TestResyncLocation(t *testing.T) {
	bus := memory.New()
	indexed := subscribe(bus, events.FileIndexedV1)
	analyzed := subscribe(bus, events.FileAnalyzedV1)
	s := newTestService(t, t.TempDir(), bus, &fakeAnalyzer{})

	dir := t.TempDir()
	writeStableFile(t, filepath.Join(dir, "a.mkv"), "a", time.Hour)
	writeStableFile(t, filepath.Join(dir, "b.mkv"), "bad", time.Hour)
	loc := createLocation(t, s, dir)
	scan(t, s, loc.ID)
	analyzePending(t, s)
	// Drain the events of the scan and of the analysis.
	for range 2 {
		<-indexed
	}
	nextAnalyzed(t, analyzed)

	if err := s.ResyncLocation(t.Context(), loc.ID); err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]bool)
	for range 2 {
		select {
		case p := <-indexed:
			var e queue.Envelope[events.FileEventV1]
			if err := json.Unmarshal(p, &e); err != nil {
				t.Fatal(err)
			}
			paths[e.Data.Path] = true
		case <-time.After(5 * time.Second):
			t.Fatal("missing indexed event")
		}
	}
	if !paths["a.mkv"] || !paths["b.mkv"] {
		t.Errorf("resynced paths = %v", paths)
	}
	if e := nextAnalyzed(t, analyzed); e.Data.FileID != files(t, s, loc.ID)["a.mkv"] {
		t.Errorf("resynced analysis of %s, want a.mkv", e.Data.FileID)
	}
	noMoreEvents(t, analyzed)

	if err := s.ResyncLocation(t.Context(), newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("resync of unknown location: got %v, want ErrNotFound", err)
	}
}

package storage

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/storage/client"
	"github.com/lukerops/mserver/internal/modules/storage/router"
)

func TestScanInterval(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"":   defaultScanInterval,
		"1h": time.Hour,
		"0":  0,
	} {
		got, err := scanInterval(v)
		if err != nil || got != want {
			t.Errorf("scanInterval(%q) = %v, %v; want %v", v, got, err, want)
		}
	}
}

func TestScanIntervalRejectsInvalid(t *testing.T) {
	for _, v := range []string{"often", "-1h"} {
		if _, err := scanInterval(v); err == nil {
			t.Errorf("scanInterval(%q): expected an error", v)
		}
	}
}

func TestNewRejectsInvalidScanInterval(t *testing.T) {
	t.Setenv("MSERVER_STORAGE_SCAN_INTERVAL", "often")
	if m, err := New(t.Context(), t.TempDir(), memory.New(), clients.Clients{}, nil); err == nil {
		m.Close()
		t.Fatal("New accepted an invalid scan interval")
	}
}

func TestModuleScansOnStart(t *testing.T) {
	t.Setenv("MSERVER_STORAGE_SCAN_INTERVAL", "0")
	dataDir := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}

	start := func() (*Module, *client.Client) {
		t.Helper()
		m, err := New(t.Context(), dataDir, memory.New(), clients.Clients{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(m.Mux)
		t.Cleanup(srv.Close)
		return m, client.New(srv.Client(), srv.URL)
	}

	// Register the location in a first run. Its startup scan may run
	// before the location exists.
	m, c := start()
	loc, err := c.CreateLocation(t.Context(), router.CreateLocationRequestV1{
		Driver:  "local",
		Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	// The second run scans it in the background.
	m, c = start()
	defer m.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		files, err := c.ListFiles(t.Context(), loc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("location was not scanned on start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

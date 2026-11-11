package storage

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"

	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/pubsub/memory"
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
	cfg := database.Config{DataDir: t.TempDir()}
	m, err := New(t.Context(), cfg, connect.NewServer(), memory.New(), "", nil)
	if err == nil {
		m.Close()
		t.Fatal("New accepted an invalid scan interval")
	}
}

func TestModuleScansOnStart(t *testing.T) {
	t.Setenv("MSERVER_STORAGE_SCAN_INTERVAL", "0")
	cfg := database.Config{DataDir: t.TempDir()}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := func() (*Module, storagev1connect.StorageServiceClient) {
		t.Helper()
		bus := memory.New()
		t.Cleanup(func() { bus.Close() })
		server := connect.NewServer()
		m, err := New(t.Context(), cfg, server, bus, "http://storage.test", log)
		if err != nil {
			t.Fatal(err)
		}
		c := storagev1connect.NewStorageServiceClient(connect.NewClient(connectinprocess.New(server)))
		return m, c
	}

	// Register the location in a first run. Its startup scan may run
	// before the location exists.
	m, c := start()
	created, err := c.CreateLocation(t.Context(), &storagev1.CreateLocationRequest{
		Driver:  "local",
		Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc := created.GetLocation()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	// The second run scans it in the background.
	m, c = start()
	defer m.Close()

	var file *storagev1.File
	deadline := time.Now().Add(5 * time.Second)
	for file == nil {
		files, err := c.ListFiles(t.Context(), &storagev1.ListFilesRequest{LocationId: loc.GetId()})
		if err != nil {
			t.Fatal(err)
		}
		if len(files.GetFiles()) == 1 {
			file = files.GetFiles()[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("location was not scanned on start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The data plane is served by the module.
	srv := httptest.NewServer(m.Mux)
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/locations/" + loc.GetId() + "/files/" + file.GetId())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "movie" {
		t.Errorf("content: status %d, body %q", resp.StatusCode, body)
	}
}

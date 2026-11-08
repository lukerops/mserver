package client_test

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/storage"
	"github.com/lukerops/mserver/internal/modules/storage/client"
	"github.com/lukerops/mserver/internal/modules/storage/router"
)

func TestRoundTrip(t *testing.T) {
	t.Setenv("MSERVER_STORAGE_SCAN_INTERVAL", "0")
	ctx := t.Context()
	m, err := storage.New(ctx, t.TempDir(), memory.New(), clients.Clients{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	srv := httptest.NewServer(m.Mux)
	defer srv.Close()
	c := client.New(srv.Client(), srv.URL)

	dir := t.TempDir()
	for _, name := range []string{"movie.mkv", "movie.en.srt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	loc, err := c.CreateLocation(ctx, router.CreateLocationRequestV1{
		Driver:  "local",
		Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loc.ID == uuid.Nil || loc.Driver != "local" || loc.Options["dir"] != dir {
		t.Fatalf("location = %+v", loc)
	}
	if got, err := c.GetLocation(ctx, loc.ID); err != nil || got.ID != loc.ID {
		t.Fatalf("GetLocation = %+v, %v", got, err)
	}
	if locations, err := c.ListLocations(ctx); err != nil || len(locations) != 1 {
		t.Fatalf("ListLocations = %+v, %v", locations, err)
	}

	result, err := c.ScanLocation(ctx, loc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result != (router.ScanResultV1{Added: 2}) {
		t.Fatalf("scan = %+v", result)
	}

	files, err := c.ListFiles(ctx, loc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "movie.en.srt" || files[1].Path != "movie.mkv" {
		t.Fatalf("files = %+v", files)
	}
	subtitle, movie := files[0], files[1]

	f, err := c.GetFile(ctx, movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != movie.ID || f.LocationID != loc.ID || f.Size != int64(len("movie.mkv")) {
		t.Errorf("file = %+v", f)
	}

	siblings, err := c.ListSiblings(ctx, movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(siblings) != 1 || siblings[0] != (router.SiblingV1{FileID: subtitle.ID, Path: subtitle.Path}) {
		t.Errorf("siblings = %+v", siblings)
	}

	u, err := c.Resolve(ctx, movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "file://" + filepath.ToSlash(filepath.Join(dir, "movie.mkv")); u != want {
		t.Errorf("Resolve = %q, want %q", u, want)
	}

	if _, err := c.GetFile(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("unknown file: got %v, want ErrNotFound", err)
	}
	_, err = c.CreateLocation(ctx, router.CreateLocationRequestV1{
		Driver:  "nfs",
		Options: map[string]string{"dir": t.TempDir()},
	})
	if !errors.Is(err, client.ErrInvalidArgument) {
		t.Errorf("unknown driver: got %v, want ErrInvalidArgument", err)
	}

	if err := c.ResyncLocation(ctx, loc.ID); err != nil {
		t.Errorf("ResyncLocation: %v", err)
	}
	if err := c.ResyncLocation(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("resync of unknown location: got %v, want ErrNotFound", err)
	}

	if err := c.DeleteLocation(ctx, loc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetLocation(ctx, loc.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("deleted location: got %v, want ErrNotFound", err)
	}
}

package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"

	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/modules/storage/service"
	"github.com/lukerops/mserver/internal/modules/storage/store"
	"github.com/lukerops/mserver/internal/pubsub/memory"
)

// newTest returns a client of the handler through the in-process
// transport, and the handler itself.
func newTest(t *testing.T) (storagev1connect.StorageServiceClient, *Handler) {
	t.Helper()
	cfg := database.Config{Driver: database.SQLite, DataDir: ":memory:"}
	db, err := database.Open(t.Context(), cfg, "storage", store.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	bus := memory.New()
	t.Cleanup(func() { bus.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(service.New(store.New(db), bus, log), "http://storage.test/", log)

	server := connect.NewServer()
	storagev1connect.RegisterStorageServiceHandler(server, h)
	transport := connectinprocess.New(server)
	return storagev1connect.NewStorageServiceClient(connect.NewClient(transport)), h
}

func TestLocationsAndFiles(t *testing.T) {
	c, h := newTest(t)
	ctx := t.Context()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := c.CreateLocation(ctx, &storagev1.CreateLocationRequest{
		Driver:  "local",
		Options: map[string]string{"dir": dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc := created.GetLocation()

	if _, err := c.CreateLocation(ctx, &storagev1.CreateLocationRequest{
		Driver:  "local",
		Options: map[string]string{"dir": dir},
	}); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("duplicate location: code = %v, want AlreadyExists (%v)", connect.CodeOf(err), err)
	}

	list, err := c.ListLocations(ctx, &storagev1.ListLocationsRequest{})
	if err != nil || len(list.GetLocations()) != 1 {
		t.Fatalf("ListLocations = %v, %v", list, err)
	}
	got, err := c.GetLocation(ctx, &storagev1.GetLocationRequest{Id: loc.GetId()})
	if err != nil || got.GetLocation().GetOptions()["dir"] != dir {
		t.Fatalf("GetLocation = %v, %v", got, err)
	}

	scanned, err := c.ScanLocation(ctx, &storagev1.ScanLocationRequest{Id: loc.GetId()})
	if err != nil || scanned.GetResult().GetAdded() != 1 {
		t.Fatalf("ScanLocation = %v, %v", scanned, err)
	}
	if _, err := c.ResyncLocation(ctx, &storagev1.ResyncLocationRequest{Id: loc.GetId()}); err != nil {
		t.Fatal(err)
	}

	files, err := c.ListFiles(ctx, &storagev1.ListFilesRequest{LocationId: loc.GetId()})
	if err != nil || len(files.GetFiles()) != 1 {
		t.Fatalf("ListFiles = %v, %v", files, err)
	}
	f := files.GetFiles()[0]
	if f.GetPath() != "movie.mkv" || f.GetSize() != 10 || f.GetLocationId() != loc.GetId() {
		t.Errorf("file = %v", f)
	}
	if page, err := c.ListFiles(ctx, &storagev1.ListFilesRequest{LocationId: loc.GetId(), Offset: 1}); err != nil || len(page.GetFiles()) != 0 {
		t.Errorf("ListFiles offset 1 = %v, %v", page, err)
	}

	file, err := c.GetFile(ctx, &storagev1.GetFileRequest{Id: f.GetId()})
	if err != nil || file.GetFile().GetId() != f.GetId() {
		t.Fatalf("GetFile = %v, %v", file, err)
	}
	if _, err := c.ListSiblings(ctx, &storagev1.ListSiblingsRequest{Id: f.GetId()}); err != nil {
		t.Fatal(err)
	}

	resolved, err := c.ResolveFile(ctx, &storagev1.ResolveFileRequest{Id: f.GetId()})
	wantURL := "http://storage.test/locations/" + loc.GetId() + "/files/" + f.GetId()
	if err != nil || resolved.GetUrl() != wantURL {
		t.Fatalf("ResolveFile = %v, %v; want %s", resolved, err, wantURL)
	}

	// The data plane.
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/locations/"+loc.GetId()+"/files/"+f.GetId(), nil)
	req.Header.Set("Range", "bytes=2-4")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "234" || resp.Header.Get("Content-Type") != "video/x-matroska" {
		t.Errorf("range: status %d, body %q, type %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	resp, err = srv.Client().Get(srv.URL + "/locations/" + f.GetLocationId() + "/files/" + loc.GetId())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown file: status %d, want 404", resp.StatusCode)
	}

	if _, err := c.DeleteLocation(ctx, &storagev1.DeleteLocationRequest{Id: loc.GetId()}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetLocation(ctx, &storagev1.GetLocationRequest{Id: loc.GetId()}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("deleted location: code = %v, want NotFound", connect.CodeOf(err))
	}
}

func TestInvalidArguments(t *testing.T) {
	c, _ := newTest(t)
	ctx := t.Context()
	check := func(name string, err error, want connect.Code) {
		t.Helper()
		if got := connect.CodeOf(err); got != want {
			t.Errorf("%s: code = %v, want %v (%v)", name, got, want, err)
		}
	}
	_, err := c.GetLocation(ctx, &storagev1.GetLocationRequest{Id: "nope"})
	check("GetLocation invalid id", err, connect.CodeInvalidArgument)
	_, err = c.GetFile(ctx, &storagev1.GetFileRequest{Id: "00000000-0000-0000-0000-000000000000"})
	check("GetFile unknown", err, connect.CodeNotFound)
	_, err = c.ListFiles(ctx, &storagev1.ListFilesRequest{LocationId: "00000000-0000-0000-0000-000000000000", Limit: -1})
	check("ListFiles negative limit", err, connect.CodeInvalidArgument)
	_, err = c.CreateLocation(ctx, &storagev1.CreateLocationRequest{Driver: "nope"})
	check("CreateLocation unknown driver", err, connect.CodeInvalidArgument)
}

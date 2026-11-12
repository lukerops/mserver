package jellyfin_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	"github.com/lukerops/mserver/gen/playback/v1/playbackv1connect"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/database"
	kvmemory "github.com/lukerops/mserver/internal/kv/memory"
	"github.com/lukerops/mserver/internal/modules/identity"
	"github.com/lukerops/mserver/internal/modules/library"
	"github.com/lukerops/mserver/internal/modules/playback"
	"github.com/lukerops/mserver/internal/modules/storage"
	"github.com/lukerops/mserver/internal/modules/subtitles"
	"github.com/lukerops/mserver/internal/pubsub/memory"
)

// Properties of the files written by writeMovie, as reported by ffprobe.
const (
	movieWidth  = 1920
	movieHeight = 1080
	// movieMinDurationMs is the requested duration: the container reports
	// a little more (the last audio frame).
	movieMinDurationMs = 1000
)

// ffprobePath returns the path of ffprobe. It skips the test when ffmpeg
// or ffprobe are not installed.
func ffprobePath(t testing.TB) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	return path
}

// writeMovie writes at path (a .mkv) a 1 s movie: 1080p H.264 video
// followed by stereo AAC audio in English, the default audio stream.
func writeMovie(t testing.TB, path string) {
	t.Helper()
	ffprobePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=1920x1080:r=24:d=1",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1",
		"-map", "0:v", "-map", "1:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ac", "2",
		"-metadata:s:a:0", "language=eng",
		"-disposition:a:0", "default",
		"-shortest", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
}

// env is a set of modules in one process, as cmd/server runs them: they
// register on rpc and call each other through the in-process transport.
type env struct {
	t   *testing.T
	dir string
	rpc *connect.Server
	mux *http.ServeMux
	bus *memory.Bus
	// baseURL is the address of the HTTP server of the modules, which
	// the storage hands out in the URLs of the files.
	baseURL string
	clients clients.Clients
}

// newEnv starts the HTTP server of the modules and the clients of their
// APIs. The modules are started by the caller, in the order that the
// in-memory bus requires: consumers before producers.
func newEnv(t *testing.T) *env {
	t.Helper()
	rpc := connect.NewServer()
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpc)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	bus := memory.New()
	t.Cleanup(func() { bus.Close() })

	local := connect.NewClient(connectinprocess.New(rpc))
	return &env{
		t:       t,
		dir:     t.TempDir(),
		rpc:     rpc,
		mux:     mux,
		bus:     bus,
		baseURL: srv.URL,
		clients: clients.Clients{
			Identity: identityv1connect.NewIdentityServiceClient(local),
			Storage:  storagev1connect.NewStorageServiceClient(local),
			Library:  libraryv1connect.NewLibraryServiceClient(local),
			Playback: playbackv1connect.NewPlaybackServiceClient(local),
		},
	}
}

func (e *env) dbConfig() database.Config { return database.Config{DataDir: e.dir} }

// startIdentity runs the identity module with the account admin and the
// profile lucas.
func (e *env) startIdentity() {
	e.t.Helper()
	ident, err := identity.New(e.t.Context(), e.dbConfig(), e.rpc, e.bus, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { ident.Close() })

	_, err = e.clients.Identity.Setup(e.t.Context(), &identityv1.SetupRequest{
		Username: "admin",
		Email:    "admin@example.com",
		Password: password,
		Profile:  &identityv1.NewProfile{Handle: "lucas", DisplayName: "Lucas"},
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

// startCatalog runs the library, the subtitles, the storage and the
// playback modules.
func (e *env) startCatalog() {
	e.t.Helper()
	ctx := e.t.Context()

	// The library and the subtitles consume the events of the storage: they
	// subscribe first.
	lib, err := library.New(ctx, e.dbConfig(), e.rpc, e.bus, e.bus, e.clients.Storage, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { lib.Close() })

	subs, err := subtitles.New(ctx, e.dbConfig(), e.rpc, e.bus, e.bus, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { subs.Close() })

	stor, err := storage.New(ctx, e.dbConfig(), e.rpc, e.bus, e.baseURL, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { stor.Close() })
	e.mux.Handle("/", stor.Mux)

	sessions := kvmemory.New()
	e.t.Cleanup(func() { sessions.Close() })
	pb, err := playback.New(ctx, e.rpc, e.bus, sessions, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { pb.Close() })
}

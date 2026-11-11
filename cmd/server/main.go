// Command server runs mserver. It serves the ConnectRPC services of the
// modules over HTTP/1.1 and HTTP/2 cleartext (h2c) on the same port, so
// gRPC and Connect clients, curl and the other mserver instances can all
// reach it.
//
// The modules register their ConnectRPC services on the one server. Only
// the migrated modules (identity, storage, library and playback) exist
// yet; the others are added as they are migrated. The API plugins (so far
// the jellyfin, under /plugins/jellyfin) call the modules through their
// clients.
//
// Variables:
//
//	MSERVER_HTTP_ADDR            HTTP listen address (default ":8096")
//	MSERVER_MODULES              "all" or a comma-separated list of the
//	                             modules and plugins to run (default all)
//	MSERVER_DB_DRIVER            sqlite or postgres (default sqlite)
//	MSERVER_DATA_DIR             directory of the SQLite databases
//	                             (default "data")
//	MSERVER_POSTGRES_URL         connection URL of the Postgres
//	MSERVER_STORAGE_PUBLIC_URL   address of this server as the readers of
//	                             the files reach it (default: the listen
//	                             address, on the loopback)
//	MSERVER_<MODULE>_URL         address of a module (IDENTITY, STORAGE,
//	                             LIBRARY or PLAYBACK) when it runs in
//	                             another instance (default: the module of
//	                             this process)
//	MSERVER_LOG_LEVEL            debug, info, warn or error (default info)
//	MSERVER_LOG_FORMAT           text or json (default text)
//
// Each module reads its own variables (e.g. MSERVER_STORAGE_SCAN_INTERVAL
// and MSERVER_FFPROBE_PATH in the storage, MSERVER_PLAYBACK_SESSION_TIMEOUT
// in the playback).
//
// The shared state of the playback sessions and of the jellyfin plugin
// lives in memory (internal/kv/memory), so a single instance of each runs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/lukerops/mserver/internal/database"
	kvmemory "github.com/lukerops/mserver/internal/kv/memory"
	"github.com/lukerops/mserver/internal/logger"
	"github.com/lukerops/mserver/internal/modules/identity"
	"github.com/lukerops/mserver/internal/modules/library"
	"github.com/lukerops/mserver/internal/modules/playback"
	"github.com/lukerops/mserver/internal/modules/storage"
	"github.com/lukerops/mserver/internal/pubsub/memory"
	"github.com/lukerops/mserver/plugins/api/jellyfin"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "mserver:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	addr := os.Getenv("MSERVER_HTTP_ADDR")
	if addr == "" {
		addr = ":8096"
	}
	dataDir := os.Getenv("MSERVER_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	if err := logger.InitGlobal(os.Getenv("MSERVER_LOG_LEVEL"), os.Getenv("MSERVER_LOG_FORMAT")); err != nil {
		return fmt.Errorf("MSERVER_LOG_LEVEL/MSERVER_LOG_FORMAT: %w", err)
	}
	log := slog.Default()

	enabled, err := parseModules(os.Getenv("MSERVER_MODULES"))
	if err != nil {
		return fmt.Errorf("MSERVER_MODULES: %w", err)
	}
	driver, err := database.ParseDriver(os.Getenv("MSERVER_DB_DRIVER"))
	if err != nil {
		return fmt.Errorf("MSERVER_DB_DRIVER: %w", err)
	}
	dbCfg := database.Config{
		Driver:      driver,
		DataDir:     dataDir,
		PostgresURL: os.Getenv("MSERVER_POSTGRES_URL"),
	}
	log.Info("mserver: starting",
		slog.String("addr", addr),
		slog.String("data_dir", dataDir),
		slog.String("db_driver", string(driver)),
	)

	// Listen first: the modules need the address to hand out. Requests
	// wait in the backlog until the server starts serving.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	bus := memory.New()
	defer func() {
		if err := bus.Close(); err != nil {
			log.Error("mserver: close pubsub", slog.String("error", err.Error()))
		}
	}()

	// The modules register their services on rpc. The routes of the plain
	// HTTP data planes go to mux, next to the ConnectRPC ones.
	rpc := connect.NewServer()
	mux := http.NewServeMux()

	if enabled["identity"] {
		ident, err := identity.New(ctx, dbCfg, rpc, bus, log.With(slog.String("module", "identity")))
		if err != nil {
			return fmt.Errorf("identity: %w", err)
		}
		defer closeModule(log, "identity", ident.Close)
	}

	// The library consumes the events of the storage: with the in-memory
	// bus, it must subscribe before the storage starts publishing.
	if enabled["library"] {
		stor, err := newStorageClient(rpc, os.Getenv("MSERVER_STORAGE_URL"), enabled["storage"])
		if err != nil {
			return fmt.Errorf("library: %w", err)
		}

		lib, err := library.New(ctx, dbCfg, rpc, bus, bus, stor, log.With(slog.String("module", "library")))
		if err != nil {
			return fmt.Errorf("library: %w", err)
		}
		defer closeModule(log, "library", lib.Close)
	}

	if enabled["storage"] {
		baseURL := os.Getenv("MSERVER_STORAGE_PUBLIC_URL")
		if baseURL == "" {
			baseURL = localURL(ln.Addr())
		}
		stor, err := storage.New(ctx, dbCfg, rpc, bus, baseURL, log.With(slog.String("module", "storage")))
		if err != nil {
			return fmt.Errorf("storage: %w", err)
		}
		defer closeModule(log, "storage", stor.Close)
		mux.Handle("/", stor.Mux)
	}

	if enabled["playback"] {
		sessions := kvmemory.New()
		defer sessions.Close()

		pb, err := playback.New(ctx, rpc, bus, sessions, log.With(slog.String("module", "playback")))
		if err != nil {
			return fmt.Errorf("playback: %w", err)
		}
		defer closeModule(log, "playback", pb.Close)
	}

	// The plugins start after the modules they call.
	if enabled["jellyfin"] {
		c, err := newClients(rpc, enabled, log)
		if err != nil {
			return fmt.Errorf("jellyfin: %w", err)
		}
		state := kvmemory.New()
		defer state.Close()

		jf := jellyfin.New(c, state, nil, log.With(slog.String("plugin", "jellyfin")))
		mount(mux, "/plugins/jellyfin", jf)
	}

	connecthttp.Mount(mux, rpc)

	srv := newHTTPServer(mux, log)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("mserver: listening", slog.String("addr", ln.Addr().String()))

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Info("mserver: stopping")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newHTTPServer returns a server that accepts HTTP/1.1 and HTTP/2
// without TLS (h2c) on the same listener.
func newHTTPServer(h http.Handler, log *slog.Logger) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{
		Handler:           h,
		Protocols:         &protocols,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.With(slog.String("component", "http")).Handler(), slog.LevelWarn),
	}
}

// available are the modules migrated to ConnectRPC, and the plugins.
var available = []string{"identity", "storage", "library", "playback", "jellyfin"}

// parseModules parses MSERVER_MODULES: "all" (or empty) or a
// comma-separated list of modules.
func parseModules(v string) (map[string]bool, error) {
	enabled := make(map[string]bool)
	if v = strings.TrimSpace(v); v == "" || v == "all" {
		for _, m := range available {
			enabled[m] = true
		}
		return enabled, nil
	}
	for name := range strings.SplitSeq(v, ",") {
		name = strings.TrimSpace(name)
		if !slices.Contains(available, name) {
			return nil, fmt.Errorf("unknown module %q (available: %s)", name, strings.Join(available, ", "))
		}
		enabled[name] = true
	}
	return enabled, nil
}

// localURL is the URL to reach the listener from this process: a
// wildcard address is reached on the loopback.
func localURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if ip := net.ParseIP(host); err != nil || ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// mount serves h under prefix, with the prefix removed from the path.
func mount(mux *http.ServeMux, prefix string, h http.Handler) {
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
}

func closeModule(log *slog.Logger, name string, closeFn func() error) {
	if err := closeFn(); err != nil {
		log.Error("mserver: close module",
			slog.String("module", name),
			slog.String("error", err.Error()),
		)
	}
}

// Command server runs mserver. It serves the ConnectRPC services of the
// modules over HTTP/1.1 and HTTP/2 cleartext (h2c) on the same port, so
// gRPC and Connect clients, curl and the other mserver instances can all
// reach it.
//
// The modules register their ConnectRPC services on the one server. Only
// the migrated modules (storage) exist yet; the others are added as they
// are migrated.
//
// Variables:
//
//	MSERVER_HTTP_ADDR            HTTP listen address (default ":8096")
//	MSERVER_MODULES              "all" or a comma-separated list of the
//	                             modules to run (default all)
//	MSERVER_DB_DRIVER            sqlite or postgres (default sqlite)
//	MSERVER_DATA_DIR             directory of the SQLite databases
//	                             (default "data")
//	MSERVER_POSTGRES_URL         connection URL of the Postgres
//	MSERVER_STORAGE_PUBLIC_URL   address of this server as the readers of
//	                             the files reach it (default: the listen
//	                             address, on the loopback)
//	MSERVER_LOG_LEVEL            debug, info, warn or error (default info)
//	MSERVER_LOG_FORMAT           text or json (default text)
//
// Each module reads its own variables (e.g. MSERVER_STORAGE_SCAN_INTERVAL
// and MSERVER_FFPROBE_PATH in the storage).
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
	"github.com/lukerops/mserver/internal/logger"
	"github.com/lukerops/mserver/internal/modules/storage"
	"github.com/lukerops/mserver/internal/pubsub/memory"
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

// available are the modules migrated to ConnectRPC.
var available = []string{"storage"}

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

func closeModule(log *slog.Logger, name string, closeFn func() error) {
	if err := closeFn(); err != nil {
		log.Error("mserver: close module",
			slog.String("module", name),
			slog.String("error", err.Error()),
		)
	}
}

// Command server runs mserver as a monolith: every module and API plugin
// is served by the same HTTP server, and the modules talk to each other
// through their REST APIs on that same server. The module routes (/v1)
// require "Authorization: Bearer <MSERVER_PRIVATE_TOKEN>"; the plugin
// routes have their own authentication.
//
// Variables:
//
//	MSERVER_PRIVATE_TOKEN  token of the module routes (required, at least 32 chars)
//	MSERVER_HTTP_ADDR      HTTP listen address (default ":8096")
//	MSERVER_DATA_DIR       directory of the databases (default "data")
//	MSERVER_LOG_LEVEL      debug, info, warn or error (default info)
//	MSERVER_LOG_FORMAT     text or json (default text)
//
// Each module reads its own variables (e.g. MSERVER_STORAGE_*).
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
	"syscall"
	"time"

	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/identity"
	identityclient "github.com/lukerops/mserver/internal/modules/identity/client"
	"github.com/lukerops/mserver/internal/modules/storage"
	storageclient "github.com/lukerops/mserver/internal/modules/storage/client"

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
	token := os.Getenv("MSERVER_PRIVATE_TOKEN")
	if len(token) < minTokenLen {
		return fmt.Errorf("MSERVER_PRIVATE_TOKEN: must have at least %d characters (e.g. openssl rand -hex 32)", minTokenLen)
	}
	addr := os.Getenv("MSERVER_HTTP_ADDR")
	if addr == "" {
		addr = ":8096"
	}
	dataDir := os.Getenv("MSERVER_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	var level slog.Level
	if v := os.Getenv("MSERVER_LOG_LEVEL"); v != "" {
		if err := level.UnmarshalText([]byte(v)); err != nil {
			return fmt.Errorf("MSERVER_LOG_LEVEL: unknown level %q", v)
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	var log *slog.Logger
	switch v := os.Getenv("MSERVER_LOG_FORMAT"); v {
	case "", "text":
		log = slog.New(slog.NewTextHandler(os.Stderr, opts))
	case "json":
		log = slog.New(slog.NewJSONHandler(os.Stderr, opts))
	default:
		return fmt.Errorf("MSERVER_LOG_FORMAT: unknown format %q", v)
	}

	// Listen first: the modules call each other on this address. Requests
	// wait in the backlog until the server starts serving.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	baseURL := localURL(ln.Addr())

	// One HTTP client for every module client, so that connections are
	// reused. It never uses a proxy (the calls are internal) and sends the
	// private token on every request.
	hc := &http.Client{Transport: bearer{token: token, next: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}}}
	c := clients.Clients{
		Identity: identityclient.New(hc, baseURL),
		Storage:  storageclient.New(hc, baseURL),
	}
	bus := memory.New()

	ident, err := identity.New(ctx, dataDir, bus, c, log.With("module", "identity"))
	if err != nil {
		return err
	}
	defer closeModule(log, "identity", ident.Close)

	stor, err := storage.New(ctx, dataDir, bus, c, log.With("module", "storage"))
	if err != nil {
		return err
	}
	defer closeModule(log, "storage", stor.Close)

	jf, err := jellyfin.New(dataDir, c, log.With("plugin", "jellyfin"))
	if err != nil {
		return err
	}

	// Modules and plugins register their routes at the root. The modules
	// share the root (every module route is under /v1) and require the
	// private token; plugins are mounted under their own path.
	mux := http.NewServeMux()
	mux.Handle("/", requireToken(token, modules(ident.Mux, stor.Mux)))
	mount(mux, "/plugins/jellyfin", jf)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("mserver: listening", "addr", ln.Addr().String(), "modules", baseURL)

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

// localURL is the URL to reach the listener from this process: a
// wildcard address is reached on the loopback.
func localURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if ip := net.ParseIP(host); err != nil || ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// modules serves every request with the first module mux that has a
// route for it, or 404 when none has.
func modules(muxes ...*http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, m := range muxes {
			if _, pattern := m.Handler(r); pattern != "" {
				m.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})
}

// mount serves h under prefix, with the prefix removed from the path.
func mount(mux *http.ServeMux, prefix string, h http.Handler) {
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
}

func closeModule(log *slog.Logger, name string, closeFn func() error) {
	if err := closeFn(); err != nil {
		log.Error("mserver: close module", "module", name, "error", err)
	}
}

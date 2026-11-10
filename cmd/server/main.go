// Command server runs mserver. It serves the ConnectRPC services of the
// modules over HTTP/1.1 and HTTP/2 cleartext (h2c) on the same port, so
// gRPC and Connect clients, curl and the other mserver instances can all
// reach it.
//
// The modules are disabled while they are migrated to ConnectRPC: the
// server starts without any service registered.
//
// Variables:
//
//	MSERVER_HTTP_ADDR   HTTP listen address (default ":8096")
//	MSERVER_DATA_DIR    directory of the databases (default "data")
//	MSERVER_LOG_LEVEL   debug, info, warn or error (default info)
//	MSERVER_LOG_FORMAT  text or json (default text)
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

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/lukerops/mserver/internal/logger"
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
	log.Info("mserver: starting", "addr", addr, "data_dir", dataDir)

	bus := memory.New()
	defer func() {
		if err := bus.Close(); err != nil {
			log.Error("mserver: close pubsub", "error", err)
		}
	}()

	// The modules register their services here (e.g.
	// identityv1connect.RegisterIdentityServiceHandler(rpc, h)) once they
	// are migrated.
	rpc := connect.NewServer()

	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpc)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	srv := newHTTPServer(mux, log)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("mserver: listening", "addr", ln.Addr().String())

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
		ErrorLog:          slog.NewLogLogger(log.With("component", "http").Handler(), slog.LevelWarn),
	}
}

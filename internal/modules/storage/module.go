// Package storage is the storage module: the VFS that owns every physical
// path and indexes files into IDs. Other modules reach its control plane
// through the ConnectRPC StorageService (storagev1connect) and read the
// content of the files from the HTTP data plane (Module.Mux).
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/handler"
	"github.com/lukerops/mserver/internal/modules/storage/service"
	"github.com/lukerops/mserver/internal/modules/storage/store"
	"github.com/lukerops/mserver/internal/pubsub"
	"github.com/lukerops/mserver/plugins/storage/ffprobe"
)

// defaultScanInterval is used when MSERVER_STORAGE_SCAN_INTERVAL is unset.
const defaultScanInterval = 6 * time.Hour

// analysisInterval is how often files waiting to become stable are looked
// for. Scans that change the index also start an analysis round.
const analysisInterval = time.Minute

// Module is a running storage module.
type Module struct {
	// Mux serves the data plane: the content of the files as plain HTTP.
	// The caller mounts it next to the ConnectRPC routes.
	Mux *http.ServeMux

	db     *sql.DB
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New opens the database of the module, registers the StorageService
// handler on server and starts the background work. Events are published
// on pub. baseURL is the address of the HTTP server that serves Mux, which
// ResolveFile hands out (e.g. "http://127.0.0.1:8096").
//
// It scans every location now and then every MSERVER_STORAGE_SCAN_INTERVAL
// (a duration, default 6h; 0 disables the periodic scans). Files are
// analyzed in the background by the analyzer plugins (see newAnalyzers).
// The storage uses no other module.
func New(ctx context.Context, cfg database.Config, server *connect.Server, pub pubsub.Publisher, baseURL string, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	interval, err := scanInterval(os.Getenv("MSERVER_STORAGE_SCAN_INTERVAL"))
	if err != nil {
		return nil, err
	}

	ffprobePath := os.Getenv("MSERVER_FFPROBE_PATH")
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}

	analyzers := newAnalyzers(ctx, ffprobePath, log)
	db, err := database.Open(ctx, cfg, "storage", store.Migrations)
	if err != nil {
		return nil, err
	}

	svc := service.New(store.New(db), pub, log, analyzers...)
	h := handler.New(svc, baseURL, log)
	storagev1connect.RegisterStorageServiceHandler(server, h)

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Module{Mux: h.Mux(), db: db, cancel: cancel}
	m.wg.Go(func() { scanLoop(runCtx, svc, interval) })
	m.wg.Go(func() { svc.RunAnalysis(runCtx, analysisInterval) })
	log.InfoContext(ctx, "storage: module started",
		slog.String("db_driver", string(cfg.Driver)),
		slog.Int64("scan_interval", int64(interval)),
		slog.Int64("analysis_interval", int64(analysisInterval)),
		slog.Int64("stable_after", int64(service.StableAfter)),
		slog.String("ffprobe_path", ffprobePath),
	)
	return m, nil
}

// newAnalyzers creates the analyzer plugins:
//
//   - ffprobe (plugins/storage/ffprobe), with the executable ffprobePath
//     (MSERVER_FFPROBE_PATH, default "ffprobe" from PATH).
//
// A missing executable is not fatal: the files wait to be analyzed until
// it is installed.
func newAnalyzers(ctx context.Context, ffprobePath string, log *slog.Logger) []analyzer.Analyzer {
	if _, err := exec.LookPath(ffprobePath); err != nil {
		log.WarnContext(ctx, "storage: ffprobe not found, video files will not be analyzed",
			slog.String("path", ffprobePath),
			slog.String("error", err.Error()),
		)
	}
	return []analyzer.Analyzer{ffprobe.New(ffprobePath, log)}
}

// scanInterval parses the value of MSERVER_STORAGE_SCAN_INTERVAL. An empty
// value is the default; zero disables the periodic scans.
func scanInterval(v string) (time.Duration, error) {
	if v == "" {
		return defaultScanInterval, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("storage: MSERVER_STORAGE_SCAN_INTERVAL: invalid duration %q", v)
	}
	return d, nil
}

// scanLoop scans every location now and then every interval, until ctx is
// canceled. A zero interval scans only once.
func scanLoop(ctx context.Context, svc *service.Service, interval time.Duration) {
	svc.ScanAll(ctx)
	if interval == 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			svc.ScanAll(ctx)
		}
	}
}

// Close stops the background work and closes the database.
func (m *Module) Close() error {
	m.cancel()
	m.wg.Wait()
	return m.db.Close()
}

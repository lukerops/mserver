// Package storage is the storage module: the VFS that owns every physical
// path and indexes files into IDs. Other modules reach it only through its
// REST API (see the client package).
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/lukerops/mserver/internal/clients"
	database "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/router"
	"github.com/lukerops/mserver/internal/modules/storage/service"
	"github.com/lukerops/mserver/internal/modules/storage/store/sqlite"
	"github.com/lukerops/mserver/plugins/storage/ffprobe"
)

// defaultScanInterval is used when MSERVER_STORAGE_SCAN_INTERVAL is unset.
const defaultScanInterval = 6 * time.Hour

// analysisInterval is how often files waiting to become stable are looked
// for. Scans that change the index also start an analysis round.
const analysisInterval = time.Minute

// Module is a running storage module.
type Module struct {
	// Mux serves the storage API.
	Mux *http.ServeMux

	db     *sql.DB
	outbox *outbox.Outbox
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New opens the database <dataDir>/storage.db, starts publishing the saved
// events on pub and builds the API. It scans every location now and then
// every MSERVER_STORAGE_SCAN_INTERVAL (a duration, default 6h; 0 disables
// the periodic scans). Files are analyzed in the background by the
// analyzer plugins (see analyzers). The storage uses no other module.
func New(ctx context.Context, dataDir string, pub queue.Publisher, _ clients.Clients, log *slog.Logger) (*Module, error) {
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
	dataFile := filepath.Join(dataDir, "storage.db")
	db, err := database.Open(ctx, dataFile)
	if err != nil {
		return nil, err
	}
	ob, err := outbox.New(ctx, db, pub, log)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	st, err := sqlite.New(ctx, db)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	ob.Start(ctx)

	svc := service.New(st, ob, log, analyzers...)
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Module{Mux: router.New(svc, log), db: db, outbox: ob, cancel: cancel}
	m.wg.Go(func() { scanLoop(runCtx, svc, interval) })
	m.wg.Go(func() { svc.RunAnalysis(runCtx, analysisInterval) })
	log.InfoContext(ctx, "storage: module started", "data_file", dataFile,
		"scan_interval", interval, "analysis_interval", analysisInterval,
		"stable_after", service.StableAfter, "ffprobe_path", ffprobePath)
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
			"path", ffprobePath, "error", err)
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
func scanLoop(ctx context.Context, svc service.Service, interval time.Duration) {
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

// Close stops the background scans and the event publishing, and closes
// the database.
func (m *Module) Close() error {
	m.cancel()
	m.wg.Wait()
	m.outbox.Stop()
	return m.db.Close()
}

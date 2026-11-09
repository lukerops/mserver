// Package playback is the playback module: the playback sessions of the
// clients, from start to stop. Other modules reach it only through its
// REST API (see the client package) and its events.
package playback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lukerops/mserver/internal/clients"
	database "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/kv"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/playback/router"
	"github.com/lukerops/mserver/internal/modules/playback/service"
)

// defaultSessionTimeout is used when MSERVER_PLAYBACK_SESSION_TIMEOUT is
// unset.
const defaultSessionTimeout = 5 * time.Minute

// reaperInterval is how often idle sessions are looked for.
const reaperInterval = 30 * time.Second

// Module is a running playback module.
type Module struct {
	// Mux serves the playback API.
	Mux *http.ServeMux

	db     *sql.DB
	outbox *outbox.Outbox
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New opens the database <dataDir>/playback.db, which holds only the
// outbox, starts publishing the saved events on pub and builds the API.
// The sessions live in st, which every instance of the module shares.
// Sessions without reports for MSERVER_PLAYBACK_SESSION_TIMEOUT (a
// duration, default 5m) are stopped. The playback uses no other module.
func New(ctx context.Context, dataDir string, pub queue.Publisher, st kv.Store, _ clients.Clients, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	timeout, err := sessionTimeout(os.Getenv("MSERVER_PLAYBACK_SESSION_TIMEOUT"))
	if err != nil {
		return nil, err
	}
	db, err := database.Open(ctx, filepath.Join(dataDir, "playback.db"))
	if err != nil {
		return nil, err
	}
	ob, err := outbox.New(ctx, db, pub, log)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	ob.Start(ctx)

	svc := service.New(st, ob, log, timeout)
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Module{Mux: router.New(svc, log), db: db, outbox: ob, cancel: cancel}
	interval := min(reaperInterval, timeout)
	m.wg.Go(func() { svc.RunReaper(runCtx, interval) })
	log.InfoContext(ctx, "playback: started",
		"db_file", filepath.Join(dataDir, "playback.db"), "session_timeout", timeout,
		"reaper_interval", interval, "key_ttl", 2*timeout)
	return m, nil
}

// sessionTimeout parses the value of MSERVER_PLAYBACK_SESSION_TIMEOUT.
func sessionTimeout(v string) (time.Duration, error) {
	if v == "" {
		return defaultSessionTimeout, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("playback: MSERVER_PLAYBACK_SESSION_TIMEOUT: invalid duration %q", v)
	}
	return d, nil
}

// Close stops the reaper and the event publishing, and closes the
// database. The sessions are left in the store: they may be served by
// another instance, or stopped by its reaper.
func (m *Module) Close() error {
	m.cancel()
	m.wg.Wait()
	m.outbox.Stop()
	return m.db.Close()
}

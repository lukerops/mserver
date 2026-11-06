// Package storage implements the storage module: the VFS that owns every
// physical path and indexes files into FileIDs. Other modules depend only
// on the storage/api package.
package storage

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	database "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/api"
	"github.com/lukerops/mserver/internal/modules/storage/store/sqlite"
)

// Module is the storage module lifecycle.
type Module struct {
	cfg Config
	pub queue.Publisher
	log *slog.Logger

	db     *sql.DB
	outbox *outbox.Outbox
	svc    *Service
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates the storage module. File events are published on pub.
func New(cfg Config, pub queue.Publisher, log *slog.Logger) *Module {
	if log == nil {
		log = slog.Default()
	}
	return &Module{cfg: cfg, pub: pub, log: log}
}

// Init opens the database.
func (m *Module) Init(ctx context.Context) error {
	db, err := database.Open(ctx, filepath.Join(m.cfg.DataDir, "storage.db"))
	if err != nil {
		return err
	}
	ob, err := outbox.New(ctx, db, m.pub, m.log)
	if err != nil {
		db.Close()
		return err
	}
	st, err := sqlite.New(ctx, db)
	if err != nil {
		db.Close()
		return err
	}
	m.db, m.outbox = db, ob
	m.svc = &Service{store: st, outbox: ob, log: m.log}
	return nil
}

// Service returns the module contract. Valid after Init.
func (m *Module) Service() api.Service { return m.svc }

// Start publishes the saved events in the background and scans every
// location now and then every ScanInterval.
func (m *Module) Start(ctx context.Context) error {
	m.outbox.Start(ctx)

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	m.wg.Go(func() {
		m.svc.scanAll(runCtx)
		if m.cfg.ScanInterval == 0 {
			return
		}

		ticker := time.NewTicker(m.cfg.ScanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				m.svc.scanAll(runCtx)
			}
		}
	})
	return nil
}

// Stop waits for the background work and closes the database.
func (m *Module) Stop(ctx context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	if m.db == nil {
		return nil
	}
	m.outbox.Stop()
	err := m.db.Close()
	m.db, m.outbox, m.svc = nil, nil, nil
	return err
}

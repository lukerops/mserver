// Package library is the library module: the global catalog of media,
// built from the events of the storage. Other modules reach it only
// through its REST API (see the client package).
package library

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/lukerops/mserver/internal/clients"
	database "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/library/router"
	"github.com/lukerops/mserver/internal/modules/library/service"
	"github.com/lukerops/mserver/internal/modules/library/store/sqlite"
)

// Module is a running library module.
type Module struct {
	// Mux serves the library API.
	Mux *http.ServeMux

	db     *sql.DB
	outbox *outbox.Outbox
}

// New opens the database <dataDir>/library.db, starts publishing the
// saved events on pub, subscribes to the storage events on sub and builds
// the API. The library uses the storage (c.Storage) to create libraries.
//
// Events published on sub before New are not seen: create the library
// before the modules it consumes events from start publishing.
func New(ctx context.Context, dataDir string, pub queue.Publisher, sub queue.Subscriber, c clients.Clients, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := database.Open(ctx, filepath.Join(dataDir, "library.db"))
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

	svc := service.New(st, ob, c.Storage, log)
	for _, topic := range service.StorageTopics {
		sub.Subscribe(topic, svc.HandleStorageEvent)
	}
	return &Module{Mux: router.New(svc, log), db: db, outbox: ob}, nil
}

// Close stops publishing events and closes the database.
func (m *Module) Close() error {
	m.outbox.Stop()
	return m.db.Close()
}

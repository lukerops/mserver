// Package identity is the identity module: accounts, profiles, devices
// and sessions. Other modules reach it only through its REST API (see
// the client package).
package identity

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/lukerops/mserver/internal/clients"
	database "github.com/lukerops/mserver/internal/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/identity/router"
	"github.com/lukerops/mserver/internal/modules/identity/service"
	"github.com/lukerops/mserver/internal/modules/identity/store/sqlite"
)

// Module is a running identity module.
type Module struct {
	// Mux serves the identity API.
	Mux *http.ServeMux

	db     *sql.DB
	outbox *outbox.Outbox
}

// New opens the database <dataDir>/identity.db, starts publishing the
// saved events on pub and builds the API. The identity uses no other
// module.
func New(ctx context.Context, dataDir string, pub queue.Publisher, _ clients.Clients, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	dataFile := filepath.Join(dataDir, "identity.db")
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

	svc := service.New(st, ob, log)
	log.InfoContext(ctx, "identity: module started", "data_file", dataFile)
	return &Module{Mux: router.New(svc, log), db: db, outbox: ob}, nil
}

// Close stops publishing events and closes the database.
func (m *Module) Close() error {
	m.outbox.Stop()
	return m.db.Close()
}

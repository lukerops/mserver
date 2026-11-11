// Package identity is the identity module: accounts, profiles, devices and
// sessions. Other modules reach it through the ConnectRPC IdentityService
// (identityv1connect).
package identity

import (
	"context"
	"database/sql"
	"log/slog"

	"connectrpc.com/connect/v2"

	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/modules/identity/handler"
	"github.com/lukerops/mserver/internal/modules/identity/service"
	"github.com/lukerops/mserver/internal/modules/identity/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// Module is a running identity module.
type Module struct {
	db *sql.DB
}

// New opens the database of the module and registers the IdentityService
// handler on server. Events are published on pub. The identity uses no
// other module.
func New(ctx context.Context, cfg database.Config, server *connect.Server, pub pubsub.Publisher, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := database.Open(ctx, cfg, "identity", store.Migrations)
	if err != nil {
		return nil, err
	}

	svc := service.New(store.New(db), pub, log)
	identityv1connect.RegisterIdentityServiceHandler(server, handler.New(svc, log))

	log.InfoContext(ctx, "identity: module started", slog.String("db_driver", string(cfg.Driver)))
	return &Module{db: db}, nil
}

// Close closes the database.
func (m *Module) Close() error {
	return m.db.Close()
}

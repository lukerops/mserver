// Package library is the library module: the global catalog of media,
// built from the events of the storage. Other modules reach it through the
// ConnectRPC LibraryService (libraryv1connect).
package library

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"connectrpc.com/connect/v2"

	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/modules/library/handler"
	"github.com/lukerops/mserver/internal/modules/library/service"
	"github.com/lukerops/mserver/internal/modules/library/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// consumer is the name of the module as a consumer of events.
const consumer = "library"

// Module is a running library module.
type Module struct {
	db     *sql.DB
	cancel context.CancelFunc
}

// New opens the database of the module, registers the LibraryService
// handler on server and subscribes to the storage and subtitles events on sub. Events are
// published on pub. The library uses the storage to check and resync the
// locations of the libraries.
//
// With the in-memory bus, events published before New are not seen: create
// the library before the modules it consumes events from start publishing.
func New(ctx context.Context, cfg database.Config, server *connect.Server, pub pubsub.Publisher, sub pubsub.Subscriber, storage storagev1connect.StorageServiceClient, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := database.Open(ctx, cfg, "library", store.Migrations)
	if err != nil {
		return nil, err
	}

	svc := service.New(store.New(db), pub, storage, log)

	// The subscriptions last until Close.
	subCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	for _, topic := range service.StorageTopics {
		if err := sub.Subscribe(subCtx, consumer, topic, svc.HandleStorageEvent); err != nil {
			cancel()
			return nil, errors.Join(err, db.Close())
		}
	}
	for _, topic := range service.SubtitleTopics {
		if err := sub.Subscribe(subCtx, consumer, topic, svc.HandleSubtitleEvent); err != nil {
			cancel()
			return nil, errors.Join(err, db.Close())
		}
	}
	libraryv1connect.RegisterLibraryServiceHandler(server, handler.New(svc, log))

	log.InfoContext(ctx, "library: module started", slog.String("db_driver", string(cfg.Driver)))
	return &Module{db: db, cancel: cancel}, nil
}

// Close stops listening to the events and closes the database.
func (m *Module) Close() error {
	m.cancel()
	return m.db.Close()
}

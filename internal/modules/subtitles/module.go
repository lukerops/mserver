// Package subtitles is the subtitles module: it matches the subtitle files
// of the storage with the movies and episodes of the library, from their
// events, and publishes the matches. Other modules reach it through the
// ConnectRPC SubtitlesService (subtitlesv1connect).
package subtitles

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"connectrpc.com/connect/v2"

	"github.com/lukerops/mserver/gen/subtitles/v1/subtitlesv1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/modules/subtitles/handler"
	"github.com/lukerops/mserver/internal/modules/subtitles/service"
	"github.com/lukerops/mserver/internal/modules/subtitles/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// consumer is the name of the module as a consumer of events.
const consumer = "subtitles"

// Module is a running subtitles module.
type Module struct {
	db     *sql.DB
	cancel context.CancelFunc
}

// New opens the database of the module, registers the SubtitlesService
// handler on server and subscribes to the storage and library events on
// sub. Events are published on pub. The subtitles module uses no other
// module.
//
// With the in-memory bus, events published before New are not seen: create
// the module before the ones it consumes events from start publishing.
func New(ctx context.Context, cfg database.Config, server *connect.Server, pub pubsub.Publisher, sub pubsub.Subscriber, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := database.Open(ctx, cfg, "subtitles", store.Migrations)
	if err != nil {
		return nil, err
	}

	svc := service.New(store.New(db), pub, log)

	// The subscriptions last until Close.
	subCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	fail := func(err error) (*Module, error) {
		cancel()
		return nil, errors.Join(err, db.Close())
	}
	for _, topic := range service.StorageTopics {
		if err := sub.Subscribe(subCtx, consumer, topic, svc.HandleStorageEvent); err != nil {
			return fail(err)
		}
	}
	for _, topic := range service.LibraryTopics {
		if err := sub.Subscribe(subCtx, consumer, topic, svc.HandleLibraryEvent); err != nil {
			return fail(err)
		}
	}
	subtitlesv1connect.RegisterSubtitlesServiceHandler(server, handler.New(svc, log))

	log.InfoContext(ctx, "subtitles: module started", slog.String("db_driver", string(cfg.Driver)))
	return &Module{db: db, cancel: cancel}, nil
}

// Close stops listening to the events and closes the database.
func (m *Module) Close() error {
	m.cancel()
	return m.db.Close()
}

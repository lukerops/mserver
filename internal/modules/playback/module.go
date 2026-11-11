// Package playback is the playback module: the playback sessions of the
// clients, from start to stop. Other modules reach it through the
// ConnectRPC PlaybackService (playbackv1connect) and its events.
package playback

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/lukerops/mserver/gen/playback/v1/playbackv1connect"
	"github.com/lukerops/mserver/internal/kv"
	"github.com/lukerops/mserver/internal/modules/playback/handler"
	"github.com/lukerops/mserver/internal/modules/playback/service"
	"github.com/lukerops/mserver/internal/pubsub"
)

// defaultSessionTimeout is used when MSERVER_PLAYBACK_SESSION_TIMEOUT is
// unset.
const defaultSessionTimeout = 5 * time.Minute

// reaperInterval is how often idle sessions are looked for.
const reaperInterval = 30 * time.Second

// Module is a running playback module.
type Module struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New registers the PlaybackService handler on server and starts the reaper
// of idle sessions. Events are published on pub. The sessions live in st,
// which every instance of the module shares. Sessions without reports for
// MSERVER_PLAYBACK_SESSION_TIMEOUT (a duration, default 5m) are stopped. The
// playback uses no other module and has no database.
func New(ctx context.Context, server *connect.Server, pub pubsub.Publisher, st kv.Store, log *slog.Logger) (*Module, error) {
	if log == nil {
		log = slog.Default()
	}
	timeout, err := sessionTimeout(os.Getenv("MSERVER_PLAYBACK_SESSION_TIMEOUT"))
	if err != nil {
		return nil, err
	}

	svc := service.New(st, pub, timeout, log)
	playbackv1connect.RegisterPlaybackServiceHandler(server, handler.New(svc, log))

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Module{cancel: cancel}
	interval := min(reaperInterval, timeout)
	m.wg.Go(func() { svc.RunReaper(runCtx, interval) })

	log.InfoContext(ctx, "playback: module started",
		slog.Int64("session_timeout", int64(timeout)),
		slog.Int64("reaper_interval", int64(interval)),
		slog.Int64("key_ttl", int64(2*timeout)),
	)
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

// Close stops the reaper. The sessions are left in the store: they may be
// served by another instance, or stopped by its reaper.
func (m *Module) Close() error {
	m.cancel()
	m.wg.Wait()
	return nil
}

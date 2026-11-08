package client_test

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/clients"
	kvmemory "github.com/lukerops/mserver/internal/infra/kv/memory"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/playback"
	"github.com/lukerops/mserver/internal/modules/playback/client"
	"github.com/lukerops/mserver/internal/modules/playback/router"
)

func TestRoundTrip(t *testing.T) {
	ctx := t.Context()
	m, err := playback.New(ctx, t.TempDir(), memory.New(), kvmemory.New(), clients.Clients{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	srv := httptest.NewServer(m.Mux)
	defer srv.Close()
	c := client.New(srv.Client(), srv.URL)

	req := router.CreatePlaybackSessionRequestV1{
		AccountID:  uuid.New(),
		ProfileID:  uuid.New(),
		DeviceID:   uuid.New(),
		ItemID:     uuid.New(),
		FileID:     uuid.New(),
		PlayMethod: "direct_play",
		PositionMs: 500,
	}
	sess, err := c.CreateSession(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == uuid.Nil || sess.ItemID != req.ItemID || sess.PositionMs != 500 || sess.Sequence != 1 {
		t.Fatalf("session = %+v", sess)
	}
	if got, err := c.GetSession(ctx, sess.ID); err != nil || got.ID != sess.ID {
		t.Fatalf("GetSession = %+v, %v", got, err)
	}
	if list, err := c.ListSessions(ctx); err != nil || len(list) != 1 {
		t.Fatalf("ListSessions = %+v, %v", list, err)
	}

	got, err := c.Progress(ctx, sess.ID, router.ProgressRequestV1{PositionMs: 9000, Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.PositionMs != 9000 || !got.Paused || got.Sequence != 3 {
		t.Errorf("progress = %+v", got)
	}
	if err := c.Heartbeat(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx, sess.ID, router.StopRequestV1{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetSession(ctx, sess.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("stopped session: got %v, want ErrNotFound", err)
	}
	if err := c.Heartbeat(ctx, sess.ID); !errors.Is(err, client.ErrNotFound) {
		t.Errorf("heartbeat of stopped session: got %v, want ErrNotFound", err)
	}

	req.PlayMethod = "transcode"
	if _, err := c.CreateSession(ctx, req); !errors.Is(err, client.ErrInvalidArgument) {
		t.Errorf("transcode: got %v, want ErrInvalidArgument", err)
	}
}

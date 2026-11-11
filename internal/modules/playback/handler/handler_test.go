package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

	playbackv1 "github.com/lukerops/mserver/gen/playback/v1"
	"github.com/lukerops/mserver/internal/kv/memory"
	"github.com/lukerops/mserver/internal/modules/playback/service"
	"github.com/lukerops/mserver/internal/pubsub"
)

type discard struct{}

func (discard) Publish(context.Context, pubsub.Message) error { return nil }

func TestPage(t *testing.T) {
	for _, tt := range []struct {
		name          string
		limit, offset int32
		wantLimit     int
		wantOffset    int
		wantErr       bool
	}{
		{name: "defaults", wantLimit: maxListLimit},
		{name: "within the maximum", limit: 10, offset: 5, wantLimit: 10, wantOffset: 5},
		{name: "the maximum", limit: maxListLimit, wantLimit: maxListLimit},
		{name: "above the maximum", limit: maxListLimit + 1, wantLimit: maxListLimit},
		{name: "negative limit", limit: -1, wantErr: true},
		{name: "negative offset", offset: -1, wantErr: true},
	} {
		limit, offset, err := page(tt.limit, tt.offset)
		if tt.wantErr {
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("%s: got %v, want InvalidArgument", tt.name, err)
			}
			continue
		}
		if err != nil || limit != tt.wantLimit || offset != tt.wantOffset {
			t.Errorf("%s: page = %d, %d, %v; want %d, %d", tt.name, limit, offset, err, tt.wantLimit, tt.wantOffset)
		}
	}
}

func TestConnectError(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := memory.New()
	defer st.Close()
	h := New(service.New(st, discard{}, time.Minute, log), log)

	for _, tt := range []struct {
		name string
		err  error
		want connect.Code
	}{
		{"invalid argument", service.ErrInvalidArgument, connect.CodeInvalidArgument},
		{"not found", service.ErrNotFound, connect.CodeNotFound},
		{"unknown", errors.New("boom"), connect.CodeInternal},
	} {
		if got := connect.CodeOf(h.connectError(tt.err)); got != tt.want {
			t.Errorf("%s: code = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestInvalidIDs(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := memory.New()
	defer st.Close()
	h := New(service.New(st, discard{}, time.Minute, log), log)
	ctx := t.Context()

	if _, err := h.GetSession(ctx, &playbackv1.GetSessionRequest{Id: "nope"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("GetSession: got %v, want InvalidArgument", err)
	}
	if _, err := h.StartSession(ctx, &playbackv1.StartSessionRequest{}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("StartSession: got %v, want InvalidArgument", err)
	}
	if _, err := h.ListSessions(ctx, &playbackv1.ListSessionsRequest{Limit: -1}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("ListSessions: got %v, want InvalidArgument", err)
	}
}

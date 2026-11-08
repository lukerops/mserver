// Package client calls the playback REST API. It works the same whether
// the playback runs in the same process or in another one.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/playback/router"
)

// Errors returned by the client, mapped from the response status. Use
// errors.Is to check them.
var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("not found")
)

// Error is an error response of the API.
type Error struct {
	Status  int
	Message string
	kind    error
}

func (e *Error) Error() string { return "playback: " + e.Message }
func (e *Error) Unwrap() error { return e.kind }

// Client is safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
}

// New returns a client of the playback API at baseURL (e.g.
// "http://127.0.0.1:8096").
func New(hc *http.Client, baseURL string) *Client {
	return &Client{http: hc, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// CreateSession starts a session. The server generates its id.
func (c *Client) CreateSession(ctx context.Context, req router.CreatePlaybackSessionRequestV1) (router.PlaybackSessionV1, error) {
	var resp router.PlaybackSessionV1
	err := c.do(ctx, http.MethodPost, "/v1/playback/sessions", req, &resp)
	return resp, err
}

// ListSessions lists the active sessions, oldest first.
func (c *Client) ListSessions(ctx context.Context) ([]router.PlaybackSessionV1, error) {
	var resp router.PlaybackSessionListV1
	err := c.do(ctx, http.MethodGet, "/v1/playback/sessions", nil, &resp)
	return resp.Sessions, err
}

func (c *Client) GetSession(ctx context.Context, id uuid.UUID) (router.PlaybackSessionV1, error) {
	var resp router.PlaybackSessionV1
	err := c.do(ctx, http.MethodGet, "/v1/playback/sessions/"+id.String(), nil, &resp)
	return resp, err
}

// Progress reports the position and the pause state of a session. It
// returns ErrNotFound if the session was stopped (or expired).
func (c *Client) Progress(ctx context.Context, id uuid.UUID, req router.ProgressRequestV1) (router.PlaybackSessionV1, error) {
	var resp router.PlaybackSessionV1
	err := c.do(ctx, http.MethodPost, "/v1/playback/sessions/"+id.String()+"/progress", req, &resp)
	return resp, err
}

// Heartbeat keeps a session alive.
func (c *Client) Heartbeat(ctx context.Context, id uuid.UUID) error {
	return c.do(ctx, http.MethodPost, "/v1/playback/sessions/"+id.String()+"/heartbeat", nil, nil)
}

// Stop ends a session.
func (c *Client) Stop(ctx context.Context, id uuid.UUID, req router.StopRequestV1) error {
	return c.do(ctx, http.MethodPost, "/v1/playback/sessions/"+id.String()+"/stop", req, nil)
}

// do sends in as the JSON body (if not nil) and decodes the response into
// out (if not nil).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("playback: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("playback: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("playback: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var e router.ErrorV1
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		err := &Error{Status: resp.StatusCode, Message: e.Error}
		switch resp.StatusCode {
		case http.StatusBadRequest:
			err.kind = ErrInvalidArgument
		case http.StatusNotFound:
			err.kind = ErrNotFound
		}
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("playback: decode response: %w", err)
	}
	return nil
}

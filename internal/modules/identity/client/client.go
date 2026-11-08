// Package client calls the identity REST API. It works the same whether
// the identity runs in the same process or in another one.
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

	"github.com/lukerops/mserver/internal/modules/identity/router"
)

// Errors returned by the client, mapped from the response status. Use
// errors.Is to check them.
var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrNotFound        = errors.New("not found")
	ErrAlreadyExists   = errors.New("already exists")
)

// Error is an error response of the API.
type Error struct {
	Status  int
	Message string
	kind    error
}

func (e *Error) Error() string { return "identity: " + e.Message }
func (e *Error) Unwrap() error { return e.kind }

// Client is safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
}

// New returns a client of the identity API at baseURL (e.g.
// "http://127.0.0.1:8096").
func New(hc *http.Client, baseURL string) *Client {
	return &Client{http: hc, baseURL: strings.TrimSuffix(baseURL, "/")}
}

func (c *Client) Setup(ctx context.Context, req router.SetupRequestV1) (router.SetupResponseV1, error) {
	var resp router.SetupResponseV1
	err := c.do(ctx, http.MethodPost, "/v1/setup", req, &resp)
	return resp, err
}

// ResyncAccounts publishes again the created event of every account.
func (c *Client) ResyncAccounts(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/accounts/resync", nil, nil)
}

// ResyncProfiles publishes again the created event of every profile.
func (c *Client) ResyncProfiles(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/profiles/resync", nil, nil)
}

// Authenticate opens a session without profile.
func (c *Client) Authenticate(ctx context.Context, req router.AuthenticateRequestV1) (router.AuthenticateResponseV1, error) {
	var resp router.AuthenticateResponseV1
	err := c.do(ctx, http.MethodPost, "/v1/sessions", req, &resp)
	return resp, err
}

// ValidateToken returns the session of a token, or ErrUnauthenticated.
func (c *Client) ValidateToken(ctx context.Context, token string) (router.SessionV1, error) {
	var resp router.SessionV1
	err := c.do(ctx, http.MethodPost, "/v1/sessions/validate", router.ValidateTokenRequestV1{Token: token}, &resp)
	return resp, err
}

func (c *Client) SelectProfile(ctx context.Context, session, profile uuid.UUID) (router.SessionV1, error) {
	var resp router.SessionV1
	err := c.do(ctx, http.MethodPut, "/v1/sessions/"+session.String()+"/profile",
		router.SelectProfileRequestV1{ProfileID: profile}, &resp)
	return resp, err
}

func (c *Client) RevokeSession(ctx context.Context, session uuid.UUID) error {
	return c.do(ctx, http.MethodDelete, "/v1/sessions/"+session.String(), nil, nil)
}

func (c *Client) GetAccount(ctx context.Context, id uuid.UUID) (router.AccountV1, error) {
	var resp router.AccountV1
	err := c.do(ctx, http.MethodGet, "/v1/accounts/"+id.String(), nil, &resp)
	return resp, err
}

func (c *Client) ListProfiles(ctx context.Context, account uuid.UUID) ([]router.ProfileV1, error) {
	var resp router.ProfileListV1
	err := c.do(ctx, http.MethodGet, "/v1/accounts/"+account.String()+"/profiles", nil, &resp)
	return resp.Profiles, err
}

func (c *Client) GetProfile(ctx context.Context, id uuid.UUID) (router.ProfileV1, error) {
	var resp router.ProfileV1
	err := c.do(ctx, http.MethodGet, "/v1/profiles/"+id.String(), nil, &resp)
	return resp, err
}

// do sends in as the JSON body (if not nil) and decodes the response into
// out (if not nil).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("identity: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
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
		case http.StatusUnauthorized:
			err.kind = ErrUnauthenticated
		case http.StatusNotFound:
			err.kind = ErrNotFound
		case http.StatusConflict:
			err.kind = ErrAlreadyExists
		}
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("identity: decode response: %w", err)
	}
	return nil
}

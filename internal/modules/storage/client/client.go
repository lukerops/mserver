// Package client calls the storage REST API. It works the same whether
// the storage runs in the same process or in another one.
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

	"github.com/lukerops/mserver/internal/modules/storage/router"
)

// Errors returned by the client, mapped from the response status. Use
// errors.Is to check them.
var (
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrNotFound           = errors.New("not found")
	ErrAlreadyExists      = errors.New("already exists")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrUnavailable        = errors.New("unavailable")
)

// Error is an error response of the API.
type Error struct {
	Status  int
	Message string
	kind    error
}

func (e *Error) Error() string { return "storage: " + e.Message }
func (e *Error) Unwrap() error { return e.kind }

// Client is safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
}

// New returns a client of the storage API at baseURL (e.g.
// "http://127.0.0.1:8096").
func New(hc *http.Client, baseURL string) *Client {
	return &Client{http: hc, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// CreateLocation registers a location. It returns ErrAlreadyExists if the
// directory overlaps another location.
func (c *Client) CreateLocation(ctx context.Context, req router.CreateLocationRequestV1) (router.LocationV1, error) {
	var resp router.LocationV1
	err := c.do(ctx, http.MethodPost, "/v1/locations", req, &resp)
	return resp, err
}

func (c *Client) ListLocations(ctx context.Context) ([]router.LocationV1, error) {
	var resp router.LocationListV1
	err := c.do(ctx, http.MethodGet, "/v1/locations", nil, &resp)
	return resp.Locations, err
}

func (c *Client) GetLocation(ctx context.Context, id uuid.UUID) (router.LocationV1, error) {
	var resp router.LocationV1
	err := c.do(ctx, http.MethodGet, "/v1/locations/"+id.String(), nil, &resp)
	return resp, err
}

// DeleteLocation removes a location and its files from the index.
func (c *Client) DeleteLocation(ctx context.Context, id uuid.UUID) error {
	return c.do(ctx, http.MethodDelete, "/v1/locations/"+id.String(), nil, nil)
}

// ScanLocation synchronizes the index with the files of the location. It
// returns ErrUnavailable if the location is not accessible.
func (c *Client) ScanLocation(ctx context.Context, id uuid.UUID) (router.ScanResultV1, error) {
	var resp router.ScanResultV1
	err := c.do(ctx, http.MethodPost, "/v1/locations/"+id.String()+"/scan", nil, &resp)
	return resp, err
}

// ResyncLocation publishes again the indexed event of every file of the
// location and the analyzed event of every analysis of their current
// content, so that consumers can rebuild their projections.
func (c *Client) ResyncLocation(ctx context.Context, id uuid.UUID) error {
	return c.do(ctx, http.MethodPost, "/v1/locations/"+id.String()+"/resync", nil, nil)
}

// ListFiles lists the files of a location ordered by path.
func (c *Client) ListFiles(ctx context.Context, location uuid.UUID) ([]router.FileV1, error) {
	var resp router.FileListV1
	err := c.do(ctx, http.MethodGet, "/v1/locations/"+location.String()+"/files", nil, &resp)
	return resp.Files, err
}

func (c *Client) GetFile(ctx context.Context, id uuid.UUID) (router.FileV1, error) {
	var resp router.FileV1
	err := c.do(ctx, http.MethodGet, "/v1/files/"+id.String(), nil, &resp)
	return resp, err
}

// ListSiblings lists the other indexed files in the same directory.
func (c *Client) ListSiblings(ctx context.Context, id uuid.UUID) ([]router.SiblingV1, error) {
	var resp router.SiblingListV1
	err := c.do(ctx, http.MethodGet, "/v1/files/"+id.String()+"/siblings", nil, &resp)
	return resp.Siblings, err
}

// Resolve returns a "file://" URL to read the file from.
func (c *Client) Resolve(ctx context.Context, id uuid.UUID) (string, error) {
	var resp router.ResolveResponseV1
	err := c.do(ctx, http.MethodGet, "/v1/files/"+id.String()+"/resolve", nil, &resp)
	return resp.URL, err
}

// do sends in as the JSON body (if not nil) and decodes the response into
// out (if not nil).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("storage: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
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
		case http.StatusConflict:
			err.kind = ErrAlreadyExists
		case http.StatusPreconditionFailed:
			err.kind = ErrFailedPrecondition
		case http.StatusServiceUnavailable:
			err.kind = ErrUnavailable
		}
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("storage: decode response: %w", err)
	}
	return nil
}

// Package client calls the library REST API. It works the same whether
// the library runs in the same process or in another one.
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

	"github.com/lukerops/mserver/internal/modules/library/router"
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

func (e *Error) Error() string { return "library: " + e.Message }
func (e *Error) Unwrap() error { return e.kind }

// Client is safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
}

// New returns a client of the library API at baseURL (e.g.
// "http://127.0.0.1:8096").
func New(hc *http.Client, baseURL string) *Client {
	return &Client{http: hc, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// CreateLibrary registers a library and catalogs the files of its
// locations. It returns ErrInvalidArgument if a location does not exist
// and ErrAlreadyExists if one belongs to another library.
func (c *Client) CreateLibrary(ctx context.Context, req router.CreateLibraryRequestV1) (router.LibraryV1, error) {
	var resp router.LibraryV1
	err := c.do(ctx, http.MethodPost, "/v1/libraries", req, &resp)
	return resp, err
}

// ListLibraries lists the libraries ordered by name.
func (c *Client) ListLibraries(ctx context.Context) ([]router.LibraryV1, error) {
	var resp router.LibraryListV1
	err := c.do(ctx, http.MethodGet, "/v1/libraries", nil, &resp)
	return resp.Libraries, err
}

func (c *Client) GetLibrary(ctx context.Context, id uuid.UUID) (router.LibraryV1, error) {
	var resp router.LibraryV1
	err := c.do(ctx, http.MethodGet, "/v1/libraries/"+id.String(), nil, &resp)
	return resp, err
}

// DeleteLibrary removes a library with its movies, series, seasons and
// episodes.
func (c *Client) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
	return c.do(ctx, http.MethodDelete, "/v1/libraries/"+id.String(), nil, nil)
}

// ResyncLibrary publishes again the added event of every movie, series,
// season and episode of the library.
func (c *Client) ResyncLibrary(ctx context.Context, id uuid.UUID) error {
	return c.do(ctx, http.MethodPost, "/v1/libraries/"+id.String()+"/resync", nil, nil)
}

// The catalog methods take the parents of the items first, in the order of
// the hierarchy (library, series, season); uuid.Nil parents match any.
// When a parent is unknown, or an item does not belong to its parents,
// they return ErrNotFound.

// ListMovies returns a page of the movies of a library.
func (c *Client) ListMovies(ctx context.Context, libraryID uuid.UUID, q router.MovieQueryV1) (router.MovieListV1, error) {
	var resp router.MovieListV1
	path := "/v1/libraries/" + parentID(libraryID) + "/movies"
	if v := q.Values(); len(v) > 0 {
		path += "?" + v.Encode()
	}
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

func (c *Client) GetMovie(ctx context.Context, libraryID, id uuid.UUID) (router.MovieV1, error) {
	var resp router.MovieV1
	path := "/v1/libraries/" + parentID(libraryID) + "/movies/" + id.String()
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

// ListSeries returns a page of the series of a library.
func (c *Client) ListSeries(ctx context.Context, libraryID uuid.UUID, q router.SeriesQueryV1) (router.SeriesListV1, error) {
	var resp router.SeriesListV1
	path := "/v1/libraries/" + parentID(libraryID) + "/series"
	if v := q.Values(); len(v) > 0 {
		path += "?" + v.Encode()
	}
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

func (c *Client) GetSeries(ctx context.Context, libraryID, id uuid.UUID) (router.SeriesV1, error) {
	var resp router.SeriesV1
	path := "/v1/libraries/" + parentID(libraryID) + "/series/" + id.String()
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

// ListSeasons lists the seasons of a series, ordered by series and number.
func (c *Client) ListSeasons(ctx context.Context, libraryID, seriesID uuid.UUID, q router.SeasonQueryV1) ([]router.SeasonV1, error) {
	var resp router.SeasonListV1
	path := "/v1/libraries/" + parentID(libraryID) + "/series/" + parentID(seriesID) + "/seasons"
	if v := q.Values(); len(v) > 0 {
		path += "?" + v.Encode()
	}
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp.Seasons, err
}

func (c *Client) GetSeason(ctx context.Context, libraryID, seriesID, id uuid.UUID) (router.SeasonV1, error) {
	var resp router.SeasonV1
	path := "/v1/libraries/" + parentID(libraryID) + "/series/" + parentID(seriesID) + "/seasons/" + id.String()
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

// ListEpisodes returns a page of the episodes of a season.
func (c *Client) ListEpisodes(ctx context.Context, libraryID, seriesID, seasonID uuid.UUID, q router.EpisodeQueryV1) (router.EpisodeListV1, error) {
	var resp router.EpisodeListV1
	path := "/v1/libraries/" + parentID(libraryID) + "/series/" + parentID(seriesID) +
		"/seasons/" + parentID(seasonID) + "/episodes"
	if v := q.Values(); len(v) > 0 {
		path += "?" + v.Encode()
	}
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

func (c *Client) GetEpisode(ctx context.Context, libraryID, seriesID, seasonID, id uuid.UUID) (router.EpisodeV1, error) {
	var resp router.EpisodeV1
	path := "/v1/libraries/" + parentID(libraryID) + "/series/" + parentID(seriesID) +
		"/seasons/" + parentID(seasonID) + "/episodes/" + id.String()
	err := c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

// parentID formats the ID of a parent, uuid.Nil being the wildcard.
func parentID(id uuid.UUID) string {
	if id == uuid.Nil {
		return router.Wildcard
	}
	return id.String()
}

// do sends in as the JSON body (if not nil) and decodes the response into
// out (if not nil).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("library: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("library: %w", err)
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
		return fmt.Errorf("library: decode response: %w", err)
	}
	return nil
}

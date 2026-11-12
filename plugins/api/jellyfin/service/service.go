// Package service is the Jellyfin adapter: it talks to the modules through
// their ConnectRPC clients, converts the mserver API into the Jellyfin API
// model (the structs in api.go) and applies the Jellyfin rules. It knows
// nothing about HTTP: the handlers package parses requests and writes
// responses on top of it.
package service

import (
	"errors"
	"log/slog"

	"connectrpc.com/connect/v2"

	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	"github.com/lukerops/mserver/gen/playback/v1/playbackv1connect"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/kv"
)

const (
	ServerName  = "mserver"
	Version     = "12.4.0"
	ProductName = "Jellyfin Server"
	// ServerID identifies the server to the clients. It never changes, so
	// that they keep recognizing the server.
	ServerID = "8f3c2a1e5b7d4c6a9e0f1b2c3d4e5f60"
)

// Errors of the service. Handlers map them to HTTP statuses.
var (
	ErrNotFound        = errors.New("not found")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrUnavailable     = errors.New("unavailable")
)

// domainError carries a message and the sentinel it maps to: errors.Is
// matches the sentinel, Error returns the clean message.
type domainError struct {
	kind error
	msg  string
}

func (e *domainError) Error() string { return e.msg }
func (e *domainError) Unwrap() error { return e.kind }

func errNotFound(msg string) error        { return &domainError{ErrNotFound, msg} }
func errInvalidArgument(msg string) error { return &domainError{ErrInvalidArgument, msg} }
func errUnauthenticated(msg string) error { return &domainError{ErrUnauthenticated, msg} }
func errUnavailable(msg string) error     { return &domainError{ErrUnavailable, msg} }

// Service is the Jellyfin adapter. The library, storage and playback
// clients may be nil when their modules are not available.
type Service struct {
	identity identityv1connect.IdentityServiceClient
	library  libraryv1connect.LibraryServiceClient
	storage  storagev1connect.StorageServiceClient
	playback playbackv1connect.PlaybackServiceClient
	kv       kv.Store
	log      *slog.Logger
}

// New returns the service on the module clients. Its state lives in st,
// which every instance of the plugin must share.
func New(c clients.Clients, st kv.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		identity: c.Identity,
		library:  c.Library,
		storage:  c.Storage,
		playback: c.Playback,
		kv:       st,
		log:      log,
	}
}

// isCode reports whether err is an error of a module with the code.
func isCode(err error, code connect.Code) bool {
	return connect.CodeOf(err) == code
}

// Caller is the authenticated user of a request: a session with a selected
// profile. It is opaque to the handlers, which only pass it back.
type Caller struct {
	SessionID          string
	AccountID          string
	ProfileID          string
	DeviceID           string
	AccountDisabled    bool
	ProfileDisplayName string
}

// Device is the client data sent in the authorization of a request.
type Device struct {
	ID      string
	Name    string
	Client  string
	Version string
}

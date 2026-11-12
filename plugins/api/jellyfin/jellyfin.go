// Package jellyfin is the Jellyfin 12.4.0 API surface. Every Jellyfin
// concept (the API model, the "<login>#<profile>" username, ServerId, id
// formats) lives in this package; the modules know nothing about Jellyfin.
//
// The API is split in two layers: the service package talks to the modules
// through their ConnectRPC clients and converts the mserver API into the
// Jellyfin model, and the handlers package parses the HTTP requests and
// writes the responses on top of it.
package jellyfin

import (
	"log/slog"
	"net/http"

	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/kv"
	"github.com/lukerops/mserver/plugins/api/jellyfin/handlers"
	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// New returns the routes of the Jellyfin API. It talks to the modules only
// through their clients; the clients of the modules that are not available
// may be nil, except the identity. Its state lives in st, which every
// instance of the plugin must share. The content of the files is read from
// the storage with files (nil means a client that uses no proxy).
func New(c clients.Clients, st kv.Store, files *http.Client, log *slog.Logger) *http.ServeMux {
	return handlers.New(service.New(c, st, log), files, log)
}

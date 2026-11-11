package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"

	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	"github.com/lukerops/mserver/gen/playback/v1/playbackv1connect"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/clients"
)

// moduleURL returns the address of a module that runs in another instance
// (MSERVER_<MODULE>_URL), or "" when it runs in this process.
func moduleURL(module string) string {
	return os.Getenv("MSERVER_" + strings.ToUpper(module) + "_URL")
}

// newTransport returns the transport to the API of a module. With url set
// the module is another instance, reached over h2c; otherwise it is the
// module of this process (local), reached through the in-process transport
// of rpc.
func newTransport(module string, rpc *connect.Server, url string, local bool) (connect.Transport, error) {
	switch {
	case url != "":
		return connecthttp.NewTransport(newH2CClient(), url), nil
	case local:
		return connectinprocess.New(rpc), nil
	default:
		return nil, errors.New("the " + module + " module is not running here: set MSERVER_" + strings.ToUpper(module) + "_URL")
	}
}

// newStorageClient returns the client of the storage API for the modules
// that use it.
func newStorageClient(rpc *connect.Server, url string, local bool) (storagev1connect.StorageServiceClient, error) {
	transport, err := newTransport("storage", rpc, url, local)
	if err != nil {
		return nil, err
	}
	return storagev1connect.NewStorageServiceClient(connect.NewClient(transport)), nil
}

// newClients returns the clients of the module APIs for the API plugins.
// The identity is required. The other modules are optional: the plugins
// answer without the features of the ones that are neither running here
// nor reachable (MSERVER_<MODULE>_URL).
func newClients(rpc *connect.Server, enabled map[string]bool, log *slog.Logger) (clients.Clients, error) {
	var c clients.Clients

	transport, err := newTransport("identity", rpc, moduleURL("identity"), enabled["identity"])
	if err != nil {
		return c, err
	}
	c.Identity = identityv1connect.NewIdentityServiceClient(connect.NewClient(transport))

	optional := func(module string) connect.Transport {
		transport, err := newTransport(module, rpc, moduleURL(module), enabled[module])
		if err != nil {
			log.Warn("mserver: module unavailable to the plugins",
				slog.String("module", module),
				slog.String("error", err.Error()),
			)
			return nil
		}
		return transport
	}
	if t := optional("storage"); t != nil {
		c.Storage = storagev1connect.NewStorageServiceClient(connect.NewClient(t))
	}
	if t := optional("library"); t != nil {
		c.Library = libraryv1connect.NewLibraryServiceClient(connect.NewClient(t))
	}
	if t := optional("playback"); t != nil {
		c.Playback = playbackv1connect.NewPlaybackServiceClient(connect.NewClient(t))
	}
	return c, nil
}

// newH2CClient returns an HTTP client that speaks HTTP/2 without TLS (h2c),
// for the calls to the other instances. It never uses a proxy: the calls
// are internal.
func newH2CClient() *http.Client {
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	return &http.Client{Transport: &http.Transport{
		Protocols:           &protocols,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}}
}

// Package jellyfin is the Jellyfin 12.4.0 API surface. Every Jellyfin
// concept (DTOs, the "<login>#<profile>" username, ServerId, id formats)
// lives in this package; the modules know nothing about Jellyfin.
package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	"github.com/lukerops/mserver/gen/playback/v1/playbackv1connect"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/kv"
)

const (
	serverName  = "mserver"
	version     = "12.4.0"
	productName = "Jellyfin Server"
	// serverID identifies the server to the clients. It never changes, so
	// that they keep recognizing the server.
	serverID = "8f3c2a1e5b7d4c6a9e0f1b2c3d4e5f60"
)

type plugin struct {
	identity identityv1connect.IdentityServiceClient
	// library is nil when the library module is not available: there are
	// no items then.
	library libraryv1connect.LibraryServiceClient
	// storage is nil when the storage module is not available: nothing
	// can be streamed then.
	storage storagev1connect.StorageServiceClient
	// playback is nil when the playback module is not available: the
	// playback reports are ignored then.
	playback playbackv1connect.PlaybackServiceClient
	// kv maps the Jellyfin PlaySessionIds to playback sessions. It is
	// shared by every instance of the plugin.
	kv kv.Store
	// files reads the content of the files from the address the storage
	// resolves them to.
	files *http.Client
	log   *slog.Logger
}

// New returns the routes of the Jellyfin API. It talks to the modules only
// through their clients; the clients of the modules that are not available
// may be nil, except the identity. Its state lives in st, which every
// instance of the plugin must share. The content of the files is read from
// the storage with files (nil means a client that uses no proxy).
func New(c clients.Clients, st kv.Store, files *http.Client, log *slog.Logger) *http.ServeMux {
	if log == nil {
		log = slog.Default()
	}
	log = slog.New(requestIDHandler{log.Handler()})
	if files == nil {
		files = &http.Client{Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		}}
	}
	p := &plugin{
		identity: c.Identity,
		library:  c.Library,
		storage:  c.Storage,
		playback: c.Playback,
		kv:       st,
		files:    files,
		log:      log,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /system/info/public", p.publicSystemInfo)
	mux.HandleFunc("GET /system/info", p.systemInfo)
	mux.HandleFunc("GET /system/endpoint", p.endpointInfo)
	mux.HandleFunc("GET /system/ping", p.ping)
	mux.HandleFunc("POST /system/ping", p.ping)

	mux.HandleFunc("GET /branding/configuration", p.brandingConfiguration)
	mux.HandleFunc("GET /branding/css", p.brandingCss)
	mux.HandleFunc("GET /branding/css.css", p.brandingCss)
	mux.HandleFunc("GET /quickconnect/enabled", p.quickConnectEnabled)

	mux.HandleFunc("GET /users/public", p.publicUsers)
	mux.HandleFunc("POST /users/authenticatebyname", p.authenticateByName)
	mux.HandleFunc("GET /users/me", p.currentUser)
	mux.HandleFunc("GET /users/{userId}", p.userByID)

	mux.HandleFunc("POST /sessions/capabilities/full", p.capabilities)
	mux.HandleFunc("POST /sessions/logout", p.logout)
	mux.HandleFunc("POST /sessions/playing", p.playing)
	mux.HandleFunc("POST /sessions/playing/progress", p.progress)
	mux.HandleFunc("POST /sessions/playing/ping", p.pingPlayback)
	mux.HandleFunc("POST /sessions/playing/stopped", p.stopped)

	mux.HandleFunc("GET /getutctime", p.utcTime)
	mux.HandleFunc("GET /playback/bitratetest", p.bitrateTest)

	mux.HandleFunc("GET /displaypreferences/{displayPreferencesId}", p.displayPreferences)
	mux.HandleFunc("POST /displaypreferences/{displayPreferencesId}", p.updateDisplayPreferences)

	mux.HandleFunc("GET /userviews", p.userViews)
	mux.HandleFunc("GET /useritems/resume", p.resumeItems)

	mux.HandleFunc("GET /items", p.items)
	mux.HandleFunc("GET /items/latest", p.latestItems)
	mux.HandleFunc("GET /items/filters", p.queryFiltersLegacy)
	mux.HandleFunc("GET /items/filters2", p.queryFilters)
	mux.HandleFunc("GET /items/{itemId}", p.item)
	mux.HandleFunc("GET /items/{itemId}/ancestors", p.ancestors)
	mux.HandleFunc("GET /items/{itemId}/thememedia", p.themeMedia)
	mux.HandleFunc("GET /items/{itemId}/intros", p.emptyItems)
	mux.HandleFunc("GET /items/{itemId}/similar", p.emptyItems)
	mux.HandleFunc("GET /items/{itemId}/specialfeatures", p.emptyItemList)
	mux.HandleFunc("GET /items/{itemId}/localtrailers", p.emptyItemList)
	mux.HandleFunc("GET /items/{itemId}/collections", p.collections)
	mux.HandleFunc("GET /items/{itemId}/playbackinfo", p.playbackInfo)
	mux.HandleFunc("POST /items/{itemId}/playbackinfo", p.playbackInfo)

	// /Videos/{itemId}/stream and /Videos/{itemId}/stream.{container}: a
	// pattern cannot match part of a segment. GET also serves HEAD.
	mux.HandleFunc("GET /videos/{itemId}/{file}", p.stream)

	mux.HandleFunc("GET /shows/{seriesId}/seasons", p.seasons)
	mux.HandleFunc("GET /shows/{seriesId}/episodes", p.episodes)
	// Nothing is next up or upcoming until watchstate and metadata.
	mux.HandleFunc("GET /shows/nextup", p.emptyItems)
	mux.HandleFunc("GET /shows/upcoming", p.emptyItems)
	// No studios until metadata.
	mux.HandleFunc("GET /studios", p.emptyItems)
	// Not in the OpenAPI anymore, but jellyfin-web and jellyfin-tizen still
	// use these routes with the user in the path. The user is always the
	// authenticated profile, so they share the handlers of the routes above.
	mux.HandleFunc("GET /users/{userId}/views", p.userViews)
	mux.HandleFunc("GET /users/{userId}/items", p.items)
	mux.HandleFunc("GET /users/{userId}/items/resume", p.resumeItems)
	mux.HandleFunc("GET /users/{userId}/items/latest", p.latestItems)
	mux.HandleFunc("GET /users/{userId}/items/{itemId}", p.item)
	mux.HandleFunc("GET /users/{userId}/items/{itemId}/intros", p.emptyItems)

	// Every response allows any origin: jellyfin-web is often served from
	// another origin.
	cors := http.NewServeMux()
	cors.Handle("/", accessLog(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE, OPTIONS")
		// Like Jellyfin (AllowAnyHeader), the preflight allows whatever
		// headers the client asks for: a header missing from a fixed list
		// fails the request in the browser as a CORS error.
		if asked := r.Header.Get("Access-Control-Request-Headers"); asked != "" {
			h.Set("Access-Control-Allow-Headers", asked)
		} else {
			h.Set("Access-Control-Allow-Headers",
				"Accept, Authorization, Content-Type, Origin, Range, X-Requested-With, X-Emby-Authorization, X-Emby-Token, X-MediaBrowser-Token")
		}
		h.Add("Vary", "Access-Control-Request-Headers")
		h.Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		a := requestAuthorization(r)
		annotateClient(r, a)

		// Jellyfin uses case-insensitive path
		path := r.URL.Path
		r.URL.Path = strings.ToLower(r.URL.Path)
		r.URL.RawPath = strings.ToLower(r.URL.RawPath)

		p.checkRoute(mux, r, path, a)
		mux.ServeHTTP(w, r)
	})))
	return cors
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// isCode reports whether err is an error of a module with the code.
func isCode(err error, code connect.Code) bool {
	return connect.CodeOf(err) == code
}

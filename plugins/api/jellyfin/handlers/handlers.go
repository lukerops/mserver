// Package handlers implements the Jellyfin HTTP API on top of the service.
// It only parses requests, calls the service and writes the responses (with
// their statuses and headers). The Jellyfin model and the conversion from
// the mserver API live in the service package.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// Handler serves the Jellyfin API. files reads the content of the files
// from the address the storage resolves them to.
type Handler struct {
	svc   *service.Service
	files *http.Client
	log   *slog.Logger
}

// New returns the routes of the Jellyfin API. The content of the files is
// read from the storage with files (nil means a client that uses no proxy).
func New(svc *service.Service, files *http.Client, log *slog.Logger) *http.ServeMux {
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
	h := &Handler{svc: svc, files: files, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /system/info/public", h.publicSystemInfo)
	mux.HandleFunc("GET /system/info", h.systemInfo)
	mux.HandleFunc("GET /system/endpoint", h.endpointInfo)
	mux.HandleFunc("GET /system/ping", h.ping)
	mux.HandleFunc("POST /system/ping", h.ping)

	mux.HandleFunc("GET /branding/configuration", h.brandingConfiguration)
	mux.HandleFunc("GET /branding/css", h.brandingCss)
	mux.HandleFunc("GET /branding/css.css", h.brandingCss)
	mux.HandleFunc("GET /quickconnect/enabled", h.quickConnectEnabled)

	mux.HandleFunc("GET /users/public", h.publicUsers)
	mux.HandleFunc("POST /users/authenticatebyname", h.authenticateByName)
	mux.HandleFunc("GET /users/me", h.currentUser)
	mux.HandleFunc("GET /users/{userId}", h.userByID)

	mux.HandleFunc("POST /sessions/capabilities/full", h.capabilities)
	mux.HandleFunc("POST /sessions/logout", h.logout)
	mux.HandleFunc("POST /sessions/playing", h.playing)
	mux.HandleFunc("POST /sessions/playing/progress", h.progress)
	mux.HandleFunc("POST /sessions/playing/ping", h.pingPlayback)
	mux.HandleFunc("POST /sessions/playing/stopped", h.stopped)

	mux.HandleFunc("GET /getutctime", h.utcTime)
	mux.HandleFunc("GET /playback/bitratetest", h.bitrateTest)

	mux.HandleFunc("GET /displaypreferences/{displayPreferencesId}", h.displayPreferences)
	mux.HandleFunc("POST /displaypreferences/{displayPreferencesId}", h.updateDisplayPreferences)

	mux.HandleFunc("GET /userviews", h.userViews)
	mux.HandleFunc("GET /useritems/resume", h.resumeItems)

	mux.HandleFunc("GET /items", h.items)
	mux.HandleFunc("GET /items/latest", h.latestItems)
	mux.HandleFunc("GET /items/filters", h.queryFiltersLegacy)
	mux.HandleFunc("GET /items/filters2", h.queryFilters)
	mux.HandleFunc("GET /items/{itemId}", h.item)
	mux.HandleFunc("GET /items/{itemId}/ancestors", h.ancestors)
	mux.HandleFunc("GET /items/{itemId}/thememedia", h.themeMedia)
	mux.HandleFunc("GET /items/{itemId}/intros", h.emptyItems)
	mux.HandleFunc("GET /items/{itemId}/similar", h.emptyItems)
	mux.HandleFunc("GET /items/{itemId}/specialfeatures", h.emptyItemList)
	mux.HandleFunc("GET /items/{itemId}/localtrailers", h.emptyItemList)
	mux.HandleFunc("GET /items/{itemId}/collections", h.collections)
	mux.HandleFunc("GET /items/{itemId}/playbackinfo", h.playbackInfo)
	mux.HandleFunc("POST /items/{itemId}/playbackinfo", h.playbackInfo)

	// /Videos/{itemId}/stream and /Videos/{itemId}/stream.{container}: a
	// pattern cannot match part of a segment. GET also serves HEAD.
	mux.HandleFunc("GET /videos/{itemId}/{file}", h.stream)
	// The external subtitles: /Videos/{itemId}/{mediaSourceId}/Subtitles/
	// {index}/Stream.{format}, optionally with the start position between
	// the index and Stream.
	mux.HandleFunc("GET /videos/{itemId}/{mediaSourceId}/subtitles/{index}/{file}", h.subtitle)
	mux.HandleFunc("GET /videos/{itemId}/{mediaSourceId}/subtitles/{index}/{startPositionTicks}/{file}", h.subtitle)

	mux.HandleFunc("GET /shows/{seriesId}/seasons", h.seasons)
	mux.HandleFunc("GET /shows/{seriesId}/episodes", h.episodes)
	// Nothing is next up or upcoming until watchstate and metadata.
	mux.HandleFunc("GET /shows/nextup", h.emptyItems)
	mux.HandleFunc("GET /shows/upcoming", h.emptyItems)
	// No studios until metadata.
	mux.HandleFunc("GET /studios", h.emptyItems)
	// Not in the OpenAPI anymore, but jellyfin-web and jellyfin-tizen still
	// use these routes with the user in the path. The user is always the
	// authenticated profile, so they share the handlers of the routes above.
	mux.HandleFunc("GET /users/{userId}/views", h.userViews)
	mux.HandleFunc("GET /users/{userId}/items", h.items)
	mux.HandleFunc("GET /users/{userId}/items/resume", h.resumeItems)
	mux.HandleFunc("GET /users/{userId}/items/latest", h.latestItems)
	mux.HandleFunc("GET /users/{userId}/items/{itemId}", h.item)
	mux.HandleFunc("GET /users/{userId}/items/{itemId}/intros", h.emptyItems)

	// Every response allows any origin: jellyfin-web is often served from
	// another origin.
	cors := http.NewServeMux()
	cors.Handle("/", accessLog(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Access-Control-Allow-Origin", "*")
		header.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE, OPTIONS")
		// Like Jellyfin (AllowAnyHeader), the preflight allows whatever
		// headers the client asks for: a header missing from a fixed list
		// fails the request in the browser as a CORS error.
		if asked := r.Header.Get("Access-Control-Request-Headers"); asked != "" {
			header.Set("Access-Control-Allow-Headers", asked)
		} else {
			header.Set("Access-Control-Allow-Headers",
				"Accept, Authorization, Content-Type, Origin, Range, X-Requested-With, X-Emby-Authorization, X-Emby-Token, X-MediaBrowser-Token")
		}
		header.Add("Vary", "Access-Control-Request-Headers")
		header.Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		auth := requestAuthorization(r)
		annotateClient(r, auth)

		// Jellyfin uses case-insensitive path.
		path := r.URL.Path
		r.URL.Path = strings.ToLower(r.URL.Path)
		r.URL.RawPath = strings.ToLower(r.URL.RawPath)

		h.checkRoute(mux, r, path, auth)
		mux.ServeHTTP(w, r)
	})))
	return cors
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

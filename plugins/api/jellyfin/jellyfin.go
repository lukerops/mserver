// Package jellyfin is the Jellyfin 12.4.0 API surface. Every Jellyfin
// concept (DTOs, the "<login>#<profile>" username, ServerId, id formats)
// lives in this package; the modules know nothing about Jellyfin.
package jellyfin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/clients"
	identity "github.com/lukerops/mserver/internal/modules/identity/client"
)

const (
	serverName  = "mserver"
	version     = "12.4.0"
	productName = "Jellyfin Server"
)

type plugin struct {
	identity *identity.Client
	log      *slog.Logger
	serverID string
}

// New returns the routes of the Jellyfin API. It talks to the modules only
// through their clients.
//
// The server id is kept in <dataDir>/jellyfin-server-id, created on the
// first run, so that clients keep recognizing the server.
func New(dataDir string, c clients.Clients, log *slog.Logger) (*http.ServeMux, error) {
	if log == nil {
		log = slog.Default()
	}
	path := filepath.Join(dataDir, "jellyfin-server-id")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b = []byte(strings.ReplaceAll(uuid.NewString(), "-", ""))
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, fmt.Errorf("jellyfin: server id: %w", err)
		}
		err = os.WriteFile(path, append(b, '\n'), 0o644)
	}
	if err != nil {
		return nil, fmt.Errorf("jellyfin: server id: %w", err)
	}
	p := &plugin{identity: c.Identity, log: log, serverID: strings.TrimSpace(string(b))}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /System/Info/Public", p.publicSystemInfo)
	mux.HandleFunc("GET /System/Info", p.systemInfo)
	mux.HandleFunc("GET /System/Ping", p.ping)
	mux.HandleFunc("POST /System/Ping", p.ping)

	mux.HandleFunc("GET /Branding/Configuration", p.brandingConfiguration)
	mux.HandleFunc("GET /Branding/Css", p.brandingCss)
	mux.HandleFunc("GET /Branding/Css.css", p.brandingCss)
	mux.HandleFunc("GET /QuickConnect/Enabled", p.quickConnectEnabled)

	mux.HandleFunc("GET /Users/Public", p.publicUsers)
	mux.HandleFunc("POST /Users/AuthenticateByName", p.authenticateByName)
	mux.HandleFunc("GET /Users/Me", p.currentUser)
	mux.HandleFunc("GET /Users/{userId}", p.userByID)

	mux.HandleFunc("POST /Sessions/Capabilities/Full", p.capabilities)
	mux.HandleFunc("POST /Sessions/Logout", p.logout)

	mux.HandleFunc("GET /DisplayPreferences/{displayPreferencesId}", p.displayPreferences)
	mux.HandleFunc("POST /DisplayPreferences/{displayPreferencesId}", p.updateDisplayPreferences)

	mux.HandleFunc("GET /UserViews", p.userViews)
	mux.HandleFunc("GET /UserItems/Resume", p.resumeItems)

	// Every response allows any origin: jellyfin-web is often served from
	// another origin.
	cors := http.NewServeMux()
	cors.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers",
			"Accept, Authorization, Content-Type, Origin, X-Requested-With, X-Emby-Authorization, X-Emby-Token, X-MediaBrowser-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return cors, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

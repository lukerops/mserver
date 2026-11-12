package handlers

import (
	"io"
	"net/http"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// displayPreferences returns the default preferences: they are not stored
// yet, but jellyfin-web needs them to finish loading after login.
func (h *Handler) displayPreferences(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	client := r.URL.Query().Get("client")
	if client == "" {
		client = "emby"
	}
	writeJSON(w, http.StatusOK, service.DisplayPreferences{
		Id:              r.PathValue("displayPreferencesId"),
		SortBy:          "SortName",
		CustomPrefs:     map[string]string{},
		ScrollDirection: "Horizontal",
		ShowBackdrop:    true,
		SortOrder:       "Ascending",
		Client:          client,
	})
}

// updateDisplayPreferences accepts and discards the preferences.
func (h *Handler) updateDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1<<20))
	w.WriteHeader(http.StatusNoContent)
}

// resumeItems lists nothing to resume until the watchstate module exists.
func (h *Handler) resumeItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, service.ItemQueryResult{Items: []service.Item{}})
}

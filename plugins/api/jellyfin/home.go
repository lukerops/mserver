package jellyfin

import (
	"io"
	"net/http"
)

// displayPreferences returns the default preferences: they are not stored
// yet, but jellyfin-web needs them to finish loading after login.
func (p *plugin) displayPreferences(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	client := r.URL.Query().Get("client")
	if client == "" {
		client = "emby"
	}
	writeJSON(w, http.StatusOK, DisplayPreferencesDto{
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
func (p *plugin) updateDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1<<20))
	w.WriteHeader(http.StatusNoContent)
}

// userViews lists no library until the library module exists.
func (p *plugin) userViews(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []any{}})
}

// resumeItems lists nothing to resume until the library module exists.
func (p *plugin) resumeItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []any{}})
}

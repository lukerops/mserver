package jellyfin

import (
	"net/http"
	"net/url"
	"runtime"
	"strings"
)

func (p *plugin) publicSystemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, p.publicInfo(r))
}

func (p *plugin) systemInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, SystemInfo{
		PublicSystemInfo:           p.publicInfo(r),
		OperatingSystemDisplayName: runtime.GOOS,
		SystemArchitecture:         runtime.GOARCH,
		CompletedInstallations:     []any{},
		CastReceiverApplications:   []any{},
	})
}

func (p *plugin) publicInfo(r *http.Request) PublicSystemInfo {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return PublicSystemInfo{
		// The address the client used to reach the plugin.
		LocalAddress:           scheme + "://" + r.Host + "/plugins/jellyfin",
		ServerName:             serverName,
		Version:                version,
		ProductName:            productName,
		OperatingSystem:        runtime.GOOS,
		Id:                     p.serverID,
		StartupWizardCompleted: true,
	}
}

// basePath is the path where the plugin is mounted (e.g.
// "/plugins/jellyfin"): the part of the requested path that was stripped
// before reaching the plugin routes.
func basePath(r *http.Request) string {
	u, err := url.ParseRequestURI(r.RequestURI)
	if err != nil || !strings.HasSuffix(u.Path, r.URL.Path) {
		return ""
	}
	return strings.TrimSuffix(u.Path, r.URL.Path)
}

func (p *plugin) ping(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, serverName)
}

func (p *plugin) brandingConfiguration(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, BrandingOptionsDto{})
}

// brandingCss has no custom CSS.
func (p *plugin) brandingCss(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// quickConnectEnabled reports that Quick Connect is not supported.
func (p *plugin) quickConnectEnabled(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, false)
}

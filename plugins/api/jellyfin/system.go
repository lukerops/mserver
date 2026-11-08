package jellyfin

import (
	"net"
	"net/http"
	"net/netip"
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
		Id:                     serverID,
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

// endpointInfo tells the client whether it connects from this machine or
// from the local network (loopback, private or link-local address). The
// address is the one of the connection: behind a reverse proxy, it is the
// address of the proxy.
func (p *plugin) endpointInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, endpoint(r.RemoteAddr))
}

func endpoint(remoteAddr string) EndPointInfo {
	var info EndPointInfo
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		info.IsLocal = ip.IsLoopback()
		info.IsInNetwork = ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return info
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

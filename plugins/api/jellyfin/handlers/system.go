package handlers

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"strings"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

func (h *Handler) publicSystemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, publicInfo(r))
}

func (h *Handler) systemInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, service.SystemInfo{
		PublicSystemInfo:           publicInfo(r),
		OperatingSystemDisplayName: runtime.GOOS,
		SystemArchitecture:         runtime.GOARCH,
		CompletedInstallations:     []any{},
		CastReceiverApplications:   []any{},
	})
}

func publicInfo(r *http.Request) service.PublicSystemInfo {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return service.PublicSystemInfo{
		// The address the client used to reach the plugin.
		LocalAddress:           scheme + "://" + r.Host + "/plugins/jellyfin",
		ServerName:             service.ServerName,
		Version:                service.Version,
		ProductName:            service.ProductName,
		OperatingSystem:        runtime.GOOS,
		Id:                     service.ServerID,
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

func (h *Handler) ping(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, service.ServerName)
}

// endpointInfo tells the client whether it connects from this machine or
// from the local network (loopback, private or link-local address). The
// address is the one of the connection: behind a reverse proxy, it is the
// address of the proxy.
func (h *Handler) endpointInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, endpoint(r.RemoteAddr))
}

func endpoint(remoteAddr string) service.EndPointInfo {
	var info service.EndPointInfo
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

func (h *Handler) brandingConfiguration(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, service.BrandingOptions{})
}

// encodingConfiguration answers the encoding configuration the clients read
// (jellyfin-web and jellyfin-tizen): the plugin never transcodes, so it
// reports no hardware acceleration. GET returns the configuration; PUT saves
// it (the plugin accepts the update and keeps its own settings).
func (h *Handler) encodingConfiguration(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	if r.Method == http.MethodPut {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, service.EncodingOptions{
		EncodingThreadCount:    -1,
		HardwareDecodingCodecs: []string{},
		AllowOnDemandMetadataBasedKeyframeExtractionForExtensions: []string{},
	})
}

// brandingCss has no custom CSS.
func (h *Handler) brandingCss(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// quickConnectEnabled reports that Quick Connect is not supported.
func (h *Handler) quickConnectEnabled(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, false)
}

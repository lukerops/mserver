package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// authorization is the content of the Authorization (or
// X-Emby-Authorization) header sent by Jellyfin clients:
//
//	MediaBrowser Client="Jellyfin Web", Device="Firefox", DeviceId="...", Version="10.10.0", Token="..."
type authorization struct {
	Client   string
	Device   string
	DeviceID string
	Version  string
	Token    string
}

// requestAuthorization reads the authorization of a request. The token
// may also come in X-Emby-Token or in the api_key query parameter.
func requestAuthorization(r *http.Request) authorization {
	header := r.Header.Get("Authorization")
	if header == "" {
		header = r.Header.Get("X-Emby-Authorization")
	}
	var a authorization
	if scheme, params, ok := strings.Cut(strings.TrimSpace(header), " "); ok &&
		(strings.EqualFold(scheme, "MediaBrowser") || strings.EqualFold(scheme, "Emby")) {
		a = parseParams(params)
	}

	for _, token := range []string{
		a.Token,
		r.Header.Get("X-Emby-Token"),
		r.Header.Get("X-MediaBrowser-Token"),
		r.URL.Query().Get("api_key"),
		r.URL.Query().Get("ApiKey"),
	} {
		if token != "" {
			a.Token = token
			break
		}
	}
	return a
}

// parseParams parses `Key="value", ...`. Values may be quoted and
// URL-encoded; commas inside quotes do not split.
func parseParams(s string) authorization {
	var (
		a      authorization
		quoted bool
		start  int
	)
	for i := 0; i <= len(s); i++ {
		if i < len(s) {
			if s[i] == '"' {
				quoted = !quoted
			}
			if s[i] != ',' || quoted {
				continue
			}
		}
		key, value, _ := strings.Cut(s[start:i], "=")
		start = i + 1

		value = strings.Trim(strings.TrimSpace(value), `"`)
		if unescaped, err := url.QueryUnescape(value); err == nil {
			value = unescaped
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "client":
			a.Client = value
		case "device":
			a.Device = value
		case "deviceid":
			a.DeviceID = value
		case "version":
			a.Version = value
		case "token":
			a.Token = value
		}
	}
	return a
}

// requestDevice is the client data of an authorization.
func requestDevice(a authorization) service.Device {
	return service.Device{ID: a.DeviceID, Name: a.Device, Client: a.Client, Version: a.Version}
}

// authenticate checks the token of the request. When it fails, the error
// response is already written and ok is false.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (service.Caller, bool) {
	c, err := h.svc.Authenticate(r.Context(), requestAuthorization(r).Token)
	if err != nil {
		h.serviceError(w, r, "authenticate", err)
		return service.Caller{}, false
	}
	return c, true
}

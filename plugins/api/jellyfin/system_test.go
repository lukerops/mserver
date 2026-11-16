package jellyfin_test

import (
	"net/http"
	"testing"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// TestEncodingConfiguration covers the endpoint jellyfin-web and jellyfin-tizen
// read while starting a playback: a 404 there is logged as "endpoint not
// implemented" and makes the client fall back. GET returns the configuration
// and PUT accepts the update without changing anything.
func TestEncodingConfiguration(t *testing.T) {
	c := start(t)

	var auth struct{ AccessToken string }
	c.expect("POST", "/Users/AuthenticateByName", authHeader,
		map[string]string{"Username": "admin#lucas", "Pw": password}, &auth, http.StatusOK)
	token := authHeader + `, Token="` + auth.AccessToken + `"`

	var got service.EncodingOptions
	c.expect("GET", "/System/Configuration/encoding", token, nil, &got, http.StatusOK)
	if got.EncodingThreadCount != -1 {
		t.Errorf("EncodingThreadCount = %d, want -1", got.EncodingThreadCount)
	}
	if got.EnableHardwareEncoding || got.EnableSubtitleExtraction {
		t.Errorf("hardware encoding or subtitle extraction enabled: %+v", got)
	}

	c.expect("PUT", "/System/Configuration/encoding", token, map[string]any{}, nil, http.StatusNoContent)
}

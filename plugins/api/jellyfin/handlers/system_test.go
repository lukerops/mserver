package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lukerops/mserver/internal/clients"
	kvmemory "github.com/lukerops/mserver/internal/kv/memory"
	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// newPlugin returns the routes without modules, on an in-memory kv store.
func newPlugin(t *testing.T) *http.ServeMux {
	t.Helper()
	st := kvmemory.New()
	t.Cleanup(func() { st.Close() })
	return New(service.New(clients.Clients{}, st, nil), nil, nil)
}

func TestServerIDIsFixed(t *testing.T) {
	if len(service.ServerID) != 32 || strings.Trim(service.ServerID, "0123456789abcdef") != "" {
		t.Fatalf("server id = %q", service.ServerID)
	}
	rec := httptest.NewRecorder()
	newPlugin(t).ServeHTTP(rec, httptest.NewRequest("GET", "/System/Info/Public", nil))
	if !strings.Contains(rec.Body.String(), `"Id":"`+service.ServerID+`"`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestEndpoint(t *testing.T) {
	for addr, want := range map[string]service.EndPointInfo{
		"127.0.0.1:5000":         {IsLocal: true, IsInNetwork: true},
		"[::1]:5000":             {IsLocal: true, IsInNetwork: true},
		"[::ffff:10.0.0.2]:5000": {IsInNetwork: true},
		"192.168.1.10:5000":      {IsInNetwork: true},
		"172.16.0.1:5000":        {IsInNetwork: true},
		"[fd00::1]:5000":         {IsInNetwork: true},
		"169.254.1.1:5000":       {IsInNetwork: true},
		"8.8.8.8:5000":           {},
		"[2001:db8::1]:5000":     {},
		"garbage":                {},
	} {
		if got := endpoint(addr); got != want {
			t.Errorf("endpoint(%q) = %+v, want %+v", addr, got, want)
		}
	}
}

func TestLocalAddressHasMountPath(t *testing.T) {
	mux := newPlugin(t)
	root := http.NewServeMux()
	root.Handle("/plugins/jellyfin/", http.StripPrefix("/plugins/jellyfin", mux))

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest("GET", "http://media.local/plugins/jellyfin/System/Info/Public", nil))
	if !strings.Contains(rec.Body.String(), `"LocalAddress":"http://media.local/plugins/jellyfin"`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

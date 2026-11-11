package jellyfin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lukerops/mserver/internal/clients"
	kvmemory "github.com/lukerops/mserver/internal/kv/memory"
)

// newPlugin returns a plugin without modules, on an in-memory kv store.
func newPlugin(t *testing.T) *http.ServeMux {
	t.Helper()
	st := kvmemory.New()
	t.Cleanup(func() { st.Close() })
	return New(clients.Clients{}, st, nil, nil)
}

func TestRequestAuthorization(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", `MediaBrowser Client="Jellyfin Web", Device="Firefox%2C%20Linux", DeviceId="a,b", Version="10.10.0", Token="tok"`)
	want := authorization{Client: "Jellyfin Web", Device: "Firefox, Linux", DeviceID: "a,b", Version: "10.10.0", Token: "tok"}
	if got := requestAuthorization(r); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRequestToken(t *testing.T) {
	header := httptest.NewRequest("GET", "/", nil)
	header.Header.Set("X-Emby-Authorization", `Emby Token="tok"`)
	embyToken := httptest.NewRequest("GET", "/", nil)
	embyToken.Header.Set("X-Emby-Token", "tok")
	apiKey := httptest.NewRequest("GET", "/?api_key=tok", nil)
	apiKey.Header.Set("Authorization", "Bearer other")

	for name, got := range map[string]string{
		"X-Emby-Authorization": requestAuthorization(header).Token,
		"X-Emby-Token":         requestAuthorization(embyToken).Token,
		"api_key":              requestAuthorization(apiKey).Token,
		"ApiKey":               requestAuthorization(httptest.NewRequest("GET", "/?ApiKey=tok", nil)).Token,
	} {
		if got != "tok" {
			t.Errorf("%s: token = %q", name, got)
		}
	}
}

func TestSplitUsername(t *testing.T) {
	for in, want := range map[string][3]any{
		"admin#lucas":         {"admin", "lucas", true},
		"a#b@example.com#kid": {"a#b@example.com", "kid", true},
		" admin # lucas ":     {"admin", "lucas", true},
		"admin":               {"", "", false},
		"admin#":              {"admin", "", false},
		"#lucas":              {"", "lucas", false},
	} {
		login, handle, ok := splitUsername(in)
		if login != want[0] || handle != want[1] || ok != want[2] {
			t.Errorf("splitUsername(%q) = %q, %q, %v; want %v", in, login, handle, ok, want)
		}
	}
}

func TestServerIDIsFixed(t *testing.T) {
	if len(serverID) != 32 || strings.Trim(serverID, "0123456789abcdef") != "" {
		t.Fatalf("server id = %q", serverID)
	}
	rec := httptest.NewRecorder()
	newPlugin(t).ServeHTTP(rec, httptest.NewRequest("GET", "/System/Info/Public", nil))
	if !strings.Contains(rec.Body.String(), `"Id":"`+serverID+`"`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestEndpoint(t *testing.T) {
	for addr, want := range map[string]EndPointInfo{
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

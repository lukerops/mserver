package handlers

import (
	"net/http/httptest"
	"testing"
)

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

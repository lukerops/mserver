package jellyfin_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/identity"
	identityclient "github.com/lukerops/mserver/internal/modules/identity/client"
	identityrouter "github.com/lukerops/mserver/internal/modules/identity/router"
	"github.com/lukerops/mserver/plugins/api/jellyfin"
)

const (
	password   = "correct horse"
	authHeader = `MediaBrowser Client="Jellyfin Web", Device="Firefox", DeviceId="dev-1", Version="10.10.0"`
)

type client struct {
	t    *testing.T
	base string
	// modules are the clients of the modules behind the plugin, if any.
	modules clients.Clients
}

// start serves the plugin on an identity with the account admin and the
// profile lucas. The identity runs behind its own HTTP server, as it
// would in another process.
func start(t *testing.T) client {
	t.Helper()
	dir := t.TempDir()
	ident, err := identity.New(t.Context(), dir, memory.New(), clients.Clients{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ident.Close() })
	identitySrv := httptest.NewServer(ident.Mux)
	t.Cleanup(identitySrv.Close)
	c := clients.Clients{Identity: identityclient.New(identitySrv.Client(), identitySrv.URL)}

	_, err = c.Identity.Setup(t.Context(), identityrouter.SetupRequestV1{
		Username: "admin",
		Email:    "admin@example.com",
		Password: password,
		Profile:  identityrouter.NewProfileV1{Handle: "lucas", DisplayName: "Lucas"},
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(jellyfin.New(c, nil, nil))
	t.Cleanup(srv.Close)
	return client{t: t, base: srv.URL}
}

// expect sends a request, checks the status and decodes a JSON response
// into out, if not nil.
func (c client) expect(method, path, auth string, body, out any, want int) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		c.t.Fatalf("%s %s = %d, want %d", method, path, res.StatusCode, want)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" {
		c.t.Fatalf("%s %s: no CORS header", method, path)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			c.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
}

func TestLoginAndHome(t *testing.T) {
	c := start(t)

	var public struct {
		Id, Version, ProductName string
		StartupWizardCompleted   bool
	}
	c.expect("GET", "/System/Info/Public", "", nil, &public, http.StatusOK)
	if len(public.Id) != 32 || public.Version != "12.4.0" || public.ProductName != "Jellyfin Server" || !public.StartupWizardCompleted {
		t.Fatalf("public info = %+v", public)
	}
	c.expect("OPTIONS", "/Users/AuthenticateByName", "", nil, nil, http.StatusNoContent)
	c.expect("GET", "/Branding/Configuration", "", nil, nil, http.StatusOK)
	c.expect("GET", "/Branding/Css", "", nil, nil, http.StatusNoContent)
	var quick bool
	c.expect("GET", "/QuickConnect/Enabled", "", nil, &quick, http.StatusOK)
	var users []any
	c.expect("GET", "/Users/Public", "", nil, &users, http.StatusOK)
	if quick || users == nil || len(users) != 0 {
		t.Fatalf("quick connect = %v, public users = %v", quick, users)
	}

	for _, username := range []string{"admin", "admin#", "admin#other", "nobody#lucas"} {
		body := map[string]string{"Username": username, "Pw": password}
		c.expect("POST", "/Users/AuthenticateByName", authHeader, body, nil, http.StatusUnauthorized)
	}
	c.expect("POST", "/Users/AuthenticateByName", authHeader,
		map[string]string{"Username": "admin#lucas", "Pw": "wrong password"}, nil, http.StatusUnauthorized)

	var auth struct {
		User struct {
			Id, Name, ServerId string
			Policy             struct{ IsAdministrator bool }
		}
		SessionInfo struct{ UserId, DeviceId, Client string }
		AccessToken string
		ServerId    string
	}
	c.expect("POST", "/Users/AuthenticateByName", authHeader,
		map[string]string{"Username": "ADMIN#Lucas", "Pw": password}, &auth, http.StatusOK)
	if auth.AccessToken == "" || auth.ServerId != public.Id || auth.User.ServerId != public.Id ||
		auth.User.Name != "Lucas" || len(auth.User.Id) != 32 || auth.User.Policy.IsAdministrator ||
		auth.SessionInfo.UserId != auth.User.Id || auth.SessionInfo.DeviceId != "dev-1" || auth.SessionInfo.Client != "Jellyfin Web" {
		t.Fatalf("auth = %+v", auth)
	}
	token := authHeader + `, Token="` + auth.AccessToken + `"`

	c.expect("GET", "/System/Endpoint", "", nil, nil, http.StatusUnauthorized)
	var endpoint struct{ IsLocal, IsInNetwork bool }
	c.expect("GET", "/System/Endpoint", token, nil, &endpoint, http.StatusOK)
	if !endpoint.IsLocal || !endpoint.IsInNetwork {
		t.Errorf("endpoint = %+v", endpoint)
	}

	c.expect("GET", "/Users/Me", "", nil, nil, http.StatusUnauthorized)
	var me struct{ Id string }
	c.expect("GET", "/Users/Me", token, nil, &me, http.StatusOK)
	c.expect("GET", "/Users/"+auth.User.Id, token, nil, nil, http.StatusOK)
	c.expect("GET", "/Users/00000000000000000000000000000000", token, nil, nil, http.StatusNotFound)
	c.expect("GET", "/System/Info?api_key="+auth.AccessToken, "", nil, nil, http.StatusOK)
	c.expect("POST", "/Sessions/Capabilities/Full", token, map[string]any{"PlayableMediaTypes": []string{"Video"}}, nil, http.StatusNoContent)
	var prefs struct{ Id, Client string }
	c.expect("GET", "/DisplayPreferences/usersettings?userId="+me.Id+"&client=emby", token, nil, &prefs, http.StatusOK)
	if me.Id != auth.User.Id || prefs.Id != "usersettings" || prefs.Client != "emby" {
		t.Fatalf("me = %+v, prefs = %+v", me, prefs)
	}
	c.expect("POST", "/DisplayPreferences/usersettings", token, map[string]any{}, nil, http.StatusNoContent)

	for _, path := range []string{"/UserViews?userId=" + me.Id, "/UserItems/Resume"} {
		var items struct {
			Items            []any
			TotalRecordCount int
		}
		c.expect("GET", path, token, nil, &items, http.StatusOK)
		if items.Items == nil || len(items.Items) != 0 || items.TotalRecordCount != 0 {
			t.Fatalf("%s = %+v", path, items)
		}
	}

	c.expect("POST", "/Sessions/Logout", token, nil, nil, http.StatusNoContent)
	c.expect("GET", "/Users/Me", token, nil, nil, http.StatusUnauthorized)
}

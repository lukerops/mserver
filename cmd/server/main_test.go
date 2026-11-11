package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
)

func TestServerAcceptsH2C(t *testing.T) {
	mux := http.NewServeMux()
	connecthttp.Mount(mux, connect.NewServer())
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("pong")) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := newHTTPServer(mux, slog.Default())
	go srv.Serve(ln)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})

	for _, tc := range []struct {
		name      string
		http1     bool
		wantMajor int
	}{
		{"http1", true, 1},
		{"h2c", false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p http.Protocols
			p.SetHTTP1(tc.http1)
			p.SetUnencryptedHTTP2(!tc.http1)
			client := &http.Client{Transport: &http.Transport{Protocols: &p}}
			defer client.CloseIdleConnections()

			resp, err := client.Get("http://" + ln.Addr().String() + "/ping")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK || resp.ProtoMajor != tc.wantMajor {
				t.Errorf("got %d on HTTP/%d, want 200 on HTTP/%d", resp.StatusCode, resp.ProtoMajor, tc.wantMajor)
			}
		})
	}
}

func TestParseModules(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"", []string{"identity", "storage", "library", "playback", "jellyfin"}, false},
		{"all", []string{"identity", "storage", "library", "playback", "jellyfin"}, false},
		{"storage", []string{"storage"}, false},
		{"library", []string{"library"}, false},
		{"playback", []string{"playback"}, false},
		{"jellyfin", []string{"jellyfin"}, false},
		{"identity,storage", []string{"identity", "storage"}, false},
		{" storage , storage", []string{"storage"}, false},
		{"nope", nil, true},
		{"storage,", nil, true},
	} {
		got, err := parseModules(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseModules(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && len(got) != len(tc.want) {
			t.Errorf("parseModules(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for _, m := range tc.want {
			if !got[m] {
				t.Errorf("parseModules(%q): %s not enabled", tc.in, m)
			}
		}
	}
}

func TestLocalURL(t *testing.T) {
	for addr, want := range map[string]string{
		"[::]:8096":      "http://127.0.0.1:8096",
		"0.0.0.0:8096":   "http://127.0.0.1:8096",
		"10.0.0.2:8096":  "http://10.0.0.2:8096",
		"[::1]:8096":     "http://[::1]:8096",
		"127.0.0.1:9000": "http://127.0.0.1:9000",
	} {
		a, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		if got := localURL(a); got != want {
			t.Errorf("localURL(%s) = %s, want %s", addr, got, want)
		}
	}
}

func TestNewStorageClient(t *testing.T) {
	rpc := connect.NewServer()
	if _, err := newStorageClient(rpc, "", false); err == nil {
		t.Error("a storage that is neither local nor remote was accepted")
	}
	for name, tc := range map[string]struct {
		url   string
		local bool
	}{
		"local":  {local: true},
		"remote": {url: "http://storage.test:8096"},
		"both":   {url: "http://storage.test:8096", local: true},
	} {
		if c, err := newStorageClient(rpc, tc.url, tc.local); err != nil || c == nil {
			t.Errorf("%s: client = %v, %v", name, c, err)
		}
	}
}

func TestNewClients(t *testing.T) {
	rpc := connect.NewServer()
	log := slog.New(slog.DiscardHandler)

	if _, err := newClients(rpc, map[string]bool{"jellyfin": true}, log); err == nil {
		t.Error("a plugin without an identity was accepted")
	}

	t.Setenv("MSERVER_IDENTITY_URL", "http://identity.test:8096")
	c, err := newClients(rpc, map[string]bool{"jellyfin": true}, log)
	if err != nil || c.Identity == nil {
		t.Fatalf("identity: client = %v, %v", c.Identity, err)
	}
	if c.Storage != nil || c.Library != nil || c.Playback != nil {
		t.Errorf("clients of modules that are nowhere: %+v", c)
	}

	t.Setenv("MSERVER_PLAYBACK_URL", "http://playback.test:8096")
	c, err = newClients(rpc, map[string]bool{"jellyfin": true, "library": true, "storage": true}, log)
	if err != nil || c.Storage == nil || c.Library == nil || c.Playback == nil {
		t.Errorf("clients = %+v, %v", c, err)
	}
}

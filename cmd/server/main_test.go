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
		{"", []string{"storage"}, false},
		{"all", []string{"storage"}, false},
		{"storage", []string{"storage"}, false},
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

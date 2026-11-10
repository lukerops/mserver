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

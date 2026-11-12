package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lukerops/mserver/internal/clients"
	kvmemory "github.com/lukerops/mserver/internal/kv/memory"
	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// logRecords decodes the JSON records written to buf.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func findRecord(recs []map[string]any, msg string) map[string]any {
	for _, r := range recs {
		if r["msg"] == msg {
			return r
		}
	}
	return nil
}

func TestUnknownRoutes(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	st := kvmemory.New()
	t.Cleanup(func() { st.Close() })
	h := New(service.New(clients.Clients{}, st, nil), nil, log)

	for _, tc := range []struct {
		method, path string
		status       int
		msg, reason  string
		allow        string
	}{
		{"GET", "/Unknown/Route", http.StatusNotFound, "jellyfin: endpoint not implemented", "endpoint not implemented", ""},
		{"GET", "/socket", http.StatusNotFound, "jellyfin: endpoint not implemented", "endpoint not implemented", ""},
		{"POST", "/Items/abc", http.StatusMethodNotAllowed, "jellyfin: method not allowed", "method not allowed", "GET, HEAD"},
		{"GET", "/Sessions/Logout", http.StatusMethodNotAllowed, "jellyfin: method not allowed", "method not allowed", "POST"},
		{"GET", "/System/Info/Public", http.StatusOK, "", "", ""},
	} {
		buf.Reset()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", `MediaBrowser Client="Android TV", Device="Shield", DeviceId="d1", Version="0.17", Token="secret-token"`)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != tc.status || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s %s = %d (CORS %q), want %d", tc.method, tc.path, rec.Code,
				rec.Header().Get("Access-Control-Allow-Origin"), tc.status)
		}
		if tc.allow != "" && rec.Header().Get("Allow") != tc.allow {
			t.Errorf("%s %s: Allow = %q, want %q", tc.method, tc.path, rec.Header().Get("Allow"), tc.allow)
		}
		if strings.Contains(buf.String(), "secret-token") {
			t.Errorf("%s %s: the token is logged: %s", tc.method, tc.path, buf.String())
		}

		recs := logRecords(t, &buf)
		access := findRecord(recs, "http: request")
		if access == nil || access["client"] != "Android TV" || access["device"] != "Shield" ||
			access["device_id"] != "d1" || access["client_version"] != "0.17" {
			t.Errorf("%s %s: access log = %v", tc.method, tc.path, access)
		}
		if tc.msg == "" {
			if access["error"] != nil {
				t.Errorf("%s %s: unexpected error %v", tc.method, tc.path, access["error"])
			}
			continue
		}
		wantLevel := "WARN"
		if tc.path == "/socket" {
			wantLevel = "DEBUG"
			if access["level"] != "DEBUG" {
				t.Errorf("%s %s: access log level = %v, want DEBUG", tc.method, tc.path, access["level"])
			}
		}
		warn := findRecord(recs, tc.msg)
		if warn == nil || warn["level"] != wantLevel || warn["path"] != tc.path || warn["method"] != tc.method ||
			warn["client"] != "Android TV" || warn["request_id"] == nil {
			t.Errorf("%s %s: warn = %v", tc.method, tc.path, warn)
		}
		if access["error"] != tc.reason {
			t.Errorf("%s %s: access error = %v, want %q", tc.method, tc.path, access["error"], tc.reason)
		}
	}
}

func TestDecodeBody(t *testing.T) {
	var buf bytes.Buffer
	p := &Handler{log: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}

	decode := func(path, body string, credentials bool) (*httptest.ResponseRecorder, bool) {
		buf.Reset()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		w := httptest.NewRecorder()
		var v struct{ AudioStreamIndex int }
		if credentials {
			return w, p.decodeCredentials(w, r, &v)
		}
		return w, p.decodeBody(w, r, &v)
	}

	if _, ok := decode("/sessions/playing", "", false); !ok {
		t.Error("empty body: want accepted")
	}
	if w, ok := decode("/users/authenticatebyname", "", true); ok || w.Code != http.StatusBadRequest {
		t.Errorf("empty credentials: ok %v, status %d", ok, w.Code)
	}

	w, ok := decode("/items/x/playbackinfo", `{"AudioStreamIndex":"1"}`, false)
	if ok || w.Code != http.StatusBadRequest || strings.TrimSpace(w.Body.String()) != "invalid body" {
		t.Fatalf("invalid body: ok %v, %d %q", ok, w.Code, w.Body)
	}
	warn := findRecord(logRecords(t, &buf), "jellyfin: invalid request body")
	if warn == nil || warn["level"] != "WARN" || warn["body"] != `{"AudioStreamIndex":"1"}` || warn["error"] == nil {
		t.Errorf("warn = %v", warn)
	}

	long := `{"AudioStreamIndex":"` + strings.Repeat("x", 3*maxLoggedBody) + `"}`
	decode("/sessions/playing", long, false)
	if warn := findRecord(logRecords(t, &buf), "jellyfin: invalid request body"); warn == nil ||
		len(warn["body"].(string)) > maxLoggedBody+len("…") {
		t.Errorf("long body not truncated: %d bytes", len(warn["body"].(string)))
	}

	decode("/users/authenticatebyname", `{"Username":"a#b","Pw":"hunter2",`, true)
	if strings.Contains(buf.String(), "hunter2") || findRecord(logRecords(t, &buf), "jellyfin: invalid request body") == nil {
		t.Errorf("credentials body logged or no warn: %s", buf.String())
	}

	p.log = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	decode("/sessions/playing", `{"AudioStreamIndex":"1"}`, false)
	if warn := findRecord(logRecords(t, &buf), "jellyfin: invalid request body"); warn == nil || warn["body"] != nil {
		t.Errorf("body logged above debug level: %v", warn)
	}
}

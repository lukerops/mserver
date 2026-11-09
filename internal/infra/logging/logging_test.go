package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// records decodes the JSON lines written to buf.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
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

func TestNew(t *testing.T) {
	if _, err := New(io.Discard, "verbose", ""); err == nil {
		t.Error("unknown level: want error")
	}
	if _, err := New(io.Discard, "", "xml"); err == nil {
		t.Error("unknown format: want error")
	}
	var buf bytes.Buffer
	log, err := New(&buf, "debug", "json")
	if err != nil {
		t.Fatal(err)
	}
	log.With("module", "x").DebugContext(WithRequestID(context.Background(), "abc"), "hello")
	log.Debug("no request")
	recs := records(t, &buf)
	if len(recs) != 2 || recs[0]["request_id"] != "abc" || recs[0]["module"] != "x" || recs[1]["request_id"] != nil {
		t.Errorf("records = %v", recs)
	}
}

func TestHandler(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(&buf, "debug", "json")
	if err != nil {
		t.Fatal(err)
	}
	var inner string
	h := Handler(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner = RequestID(r.Context())
		switch r.URL.Path {
		case "/panic":
			panic("boom")
		case "/missing":
			Annotate(r.Context(), "error", "no such item", "client", "Jellyfin Web")
			http.NotFound(w, r)
		default:
			_, _ = io.Copy(w, strings.NewReader("hello"))
		}
	}))

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest("GET", "/ok?api_key=secret&Size=5", nil))
	if res.Code != 200 || res.Body.String() != "hello" || res.Header().Get(RequestIDHeader) != inner || len(inner) != 16 {
		t.Errorf("ok: %d %q, id %q / %q", res.Code, res.Body, res.Header().Get(RequestIDHeader), inner)
	}

	req := httptest.NewRequest("GET", "/missing", nil)
	req.Header.Set(RequestIDHeader, "from-caller")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if inner != "from-caller" {
		t.Errorf("request id = %q, want the one of the caller", inner)
	}

	res = httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest("POST", "/panic", nil))
	if res.Code != 500 {
		t.Errorf("panic: status %d", res.Code)
	}

	recs := records(t, &buf)
	if len(recs) != 4 {
		t.Fatalf("records = %v", recs)
	}
	ok, missing, panicked, failed := recs[0], recs[1], recs[2], recs[3]
	if ok["level"] != "DEBUG" || ok["status"] != 200.0 || ok["bytes"] != 5.0 ||
		ok["query"] != "Size=5&api_key=REDACTED" || ok["method"] != "GET" || ok["path"] != "/ok" {
		t.Errorf("ok = %v", ok)
	}
	if strings.Contains(buf.String(), "secret") {
		t.Error("the api key is logged")
	}
	if missing["level"] != "INFO" || missing["status"] != 404.0 || missing["error"] != "no such item" ||
		missing["client"] != "Jellyfin Web" || missing["request_id"] != "from-caller" {
		t.Errorf("missing = %v", missing)
	}
	if panicked["level"] != "ERROR" || panicked["panic"] != "boom" || panicked["stack"] == "" {
		t.Errorf("panic = %v", panicked)
	}
	if failed["level"] != "ERROR" || failed["status"] != 500.0 {
		t.Errorf("failed = %v", failed)
	}
}

func TestTransport(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(RequestIDHeader)
	}))
	defer srv.Close()
	c := &http.Client{Transport: Transport{}}

	req, _ := http.NewRequestWithContext(WithRequestID(context.Background(), "abc"), "GET", srv.URL, nil)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got != "abc" {
		t.Errorf("forwarded id = %q", got)
	}
	if req.Header.Get(RequestIDHeader) != "" {
		t.Error("the request of the caller was changed")
	}
}

var _ slog.Handler = contextHandler{}

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModules(t *testing.T) {
	a := http.NewServeMux()
	a.HandleFunc("GET /v1/accounts/{id}", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("a")) })
	b := http.NewServeMux()
	b.HandleFunc("GET /v1/locations", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("b")) })
	h := modules(a, b)

	for _, tc := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/v1/accounts/1", http.StatusOK, "a"},
		{"GET", "/v1/locations", http.StatusOK, "b"},
		{"POST", "/v1/locations", http.StatusNotFound, ""},
		{"GET", "/v1/unknown", http.StatusNotFound, ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.status || (tc.body != "" && rec.Body.String() != tc.body) {
			t.Errorf("%s %s = %d %q, want %d %q", tc.method, tc.path, rec.Code, rec.Body, tc.status, tc.body)
		}
	}
}

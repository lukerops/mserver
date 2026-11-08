package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestRequireToken(t *testing.T) {
	h := requireToken(testToken, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name, auth string
		status     int
	}{
		{"valid", "Bearer " + testToken, http.StatusNoContent},
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer " + testToken[1:] + "x", http.StatusUnauthorized},
		{"prefix", "Bearer " + testToken[:10], http.StatusUnauthorized},
		{"no scheme", testToken, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/v1/accounts/1", nil)
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestBearerTransport(t *testing.T) {
	srv := httptest.NewServer(requireToken(testToken, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	defer srv.Close()

	for _, tc := range []struct {
		name   string
		hc     *http.Client
		status int
	}{
		{"with token", &http.Client{Transport: bearer{token: testToken, next: http.DefaultTransport}}, http.StatusNoContent},
		{"without token", http.DefaultClient, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", srv.URL, nil)
			resp, err := tc.hc.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if req.Header.Get("Authorization") != "" {
				t.Error("the caller's request was modified")
			}
		})
	}
}

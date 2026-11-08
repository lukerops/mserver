package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// minTokenLen is the minimum length of MSERVER_PRIVATE_TOKEN.
const minTokenLen = 32

// requireToken serves h only to requests that carry the private token as
// "Authorization: Bearer <token>"; the others get 401.
func requireToken(token string, h http.Handler) http.Handler {
	// Comparing hashes keeps the comparison constant-time even when the
	// lengths differ.
	want := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

// bearer adds the private token to every request sent through next.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

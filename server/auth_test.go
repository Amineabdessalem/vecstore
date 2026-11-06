package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAPIKeys(t *testing.T) {
	h := RequireAPIKeys(newServer(), []string{"alpha", " beta ", ""})
	cases := []struct {
		method, path, auth string
		want               int
	}{
		{"GET", "/healthz", "", http.StatusOK},
		{"GET", "/collections", "", http.StatusUnauthorized},
		{"GET", "/collections", "Bearer wrong", http.StatusUnauthorized},
		{"GET", "/collections", "alpha", http.StatusUnauthorized},
		{"GET", "/collections", "Bearer alpha", http.StatusOK},
		{"GET", "/collections", "Bearer beta", http.StatusOK},
		{"GET", "/metrics", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s auth=%q: got %d, want %d", c.method, c.path, c.auth, rec.Code, c.want)
		}
		if rec.Code == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("401 without WWW-Authenticate header")
		}
	}
}

func TestNoKeysMeansOpen(t *testing.T) {
	s := newServer()
	if RequireAPIKeys(s, nil) != http.Handler(s) {
		t.Fatal("expected the handler unchanged when no keys are configured")
	}
}

package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// RequireAPIKeys wraps h so every request except GET /healthz must carry
// "Authorization: Bearer <key>" with one of keys. With no keys it returns h unchanged.
// Keys are compared as SHA-256 digests in constant time.
func RequireAPIKeys(h http.Handler, keys []string) http.Handler {
	var digests [][32]byte
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" {
			digests = append(digests, sha256.Sum256([]byte(k)))
		}
	}
	if len(digests) == 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			h.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if ok && validKey(sha256.Sum256([]byte(token)), digests) {
			h.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="vecstore"`)
		writeJSON(w, http.StatusUnauthorized, errorBody("missing or invalid API key"))
	})
}

func validKey(got [32]byte, digests [][32]byte) bool {
	match := 0
	for _, d := range digests {
		match |= subtle.ConstantTimeCompare(got[:], d[:])
	}
	return match == 1
}

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Amineabdessalem/vecstore/index"
	"github.com/Amineabdessalem/vecstore/store"
)

func do(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: bad JSON %q", method, path, rec.Body.String())
		}
	}
	return rec.Code, out
}

func newServer() *Server {
	return New(store.New(index.DefaultHNSWConfig()), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestCollectionLifecycle(t *testing.T) {
	s := newServer()
	if code, _ := do(t, s, "POST", "/collections", `{"name":"docs","dim":3}`); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if code, _ := do(t, s, "POST", "/collections", `{"name":"docs","dim":3}`); code != http.StatusConflict {
		t.Errorf("duplicate create: %d", code)
	}
	code, body := do(t, s, "GET", "/collections/docs", "")
	if code != 200 || body["metric"] != "cosine" || body["dim"].(float64) != 3 {
		t.Errorf("get: %d %v", code, body)
	}
	if code, _ := do(t, s, "DELETE", "/collections/docs", ""); code != http.StatusNoContent {
		t.Errorf("drop: %d", code)
	}
	if code, _ := do(t, s, "GET", "/collections/docs", ""); code != http.StatusNotFound {
		t.Errorf("get after drop: %d", code)
	}
}

func TestUpsertAndSearch(t *testing.T) {
	s := newServer()
	do(t, s, "POST", "/collections", `{"name":"pts","dim":2,"metric":"euclidean"}`)
	code, body := do(t, s, "PUT", "/collections/pts/vectors", `{"vectors":[
		{"id":"a","values":[0,0],"metadata":{"tag":"x"}},
		{"id":"b","values":[1,1],"metadata":{"tag":"y"}},
		{"id":"c","values":[2,2],"metadata":{"tag":"x"}}]}`)
	if code != 200 || body["count"].(float64) != 3 {
		t.Fatalf("upsert: %d %v", code, body)
	}
	code, body = do(t, s, "POST", "/collections/pts/search", `{"vector":[1.1,1.1],"k":1}`)
	hits := body["hits"].([]any)
	if code != 200 || len(hits) != 1 || hits[0].(map[string]any)["id"] != "b" {
		t.Errorf("search: %d %v", code, body)
	}
	_, body = do(t, s, "POST", "/collections/pts/search", `{"vector":[1.1,1.1],"k":3,"filter":{"tag":"x"}}`)
	if hits := body["hits"].([]any); len(hits) != 2 {
		t.Errorf("filtered search: %v", body)
	}
	if code, _ := do(t, s, "DELETE", "/collections/pts/vectors/b", ""); code != http.StatusNoContent {
		t.Errorf("delete vector: %d", code)
	}
	if code, _ := do(t, s, "GET", "/collections/pts/vectors/b", ""); code != http.StatusNotFound {
		t.Errorf("get deleted vector: %d", code)
	}
}

func TestBadRequests(t *testing.T) {
	s := newServer()
	do(t, s, "POST", "/collections", `{"name":"v","dim":2}`)
	cases := []struct{ method, path, body string }{
		{"POST", "/collections", `{"name":"v2","dim":0}`},
		{"POST", "/collections", `{"nme":"typo"}`},
		{"PUT", "/collections/v/vectors", `{"vectors":[{"id":"a","values":[1,2,3]}]}`},
		{"POST", "/collections/v/search", `{"vector":[1],"k":1}`},
		{"POST", "/collections/v/search", `{"vector":[1,2],"k":5000}`},
		{"POST", "/collections/v/search", `not json`},
	}
	for _, tc := range cases {
		if code, body := do(t, s, tc.method, tc.path, tc.body); code != http.StatusBadRequest || body["error"] == "" {
			t.Errorf("%s %s %s: got %d %v", tc.method, tc.path, tc.body, code, body)
		}
	}
	if code, _ := do(t, s, "GET", "/healthz", ""); code != 200 {
		t.Errorf("healthz: %d", code)
	}
}

func TestRangeFilterOverHTTP(t *testing.T) {
	s := newServer()
	do(t, s, "POST", "/collections", `{"name":"papers","dim":2,"metric":"euclidean"}`)
	do(t, s, "PUT", "/collections/papers/vectors", `{"vectors":[
		{"id":"old","values":[0,0],"metadata":{"year":2015}},
		{"id":"new","values":[0.5,0],"metadata":{"year":2024}},
		{"id":"newer","values":[3,3],"metadata":{"year":2025}}]}`)
	_, body := do(t, s, "POST", "/collections/papers/search",
		`{"vector":[0,0],"k":5,"filter":{"year":{"gte":2020}}}`)
	hits := body["hits"].([]any)
	if len(hits) != 2 || hits[0].(map[string]any)["id"] != "new" {
		t.Errorf("range filter: %v", body)
	}
	code, _ := do(t, s, "POST", "/collections/papers/search", `{"vector":[0,0],"filter":{"year":{"after":2020}}}`)
	if code != 400 {
		t.Errorf("unknown operator should be 400, got %d", code)
	}
}

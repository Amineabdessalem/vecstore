package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsEndpoint(t *testing.T) {
	s := newServer()
	do(t, s, "POST", "/collections", `{"name":"docs","dim":2}`)
	do(t, s, "PUT", "/collections/docs/vectors", `{"vectors":[{"id":"a","values":[1,0]},{"id":"b","values":[0,1]}]}`)
	do(t, s, "DELETE", "/collections/docs/vectors/b", "")
	do(t, s, "GET", "/collections/missing", "")
	do(t, s, "GET", "/collections/other", "")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`vecstore_http_requests_total{route="POST /collections",status="201"} 1`,
		// Both 404s share one route label instead of one series per collection name.
		`vecstore_http_requests_total{route="GET /collections/{name}",status="404"} 2`,
		`vecstore_http_request_duration_seconds_count{route="PUT /collections/{name}/vectors"} 1`,
		`vecstore_http_request_duration_seconds_bucket{route="POST /collections",le="+Inf"} 1`,
		`vecstore_collection_vectors{collection="docs"} 1`,
		`vecstore_collection_deleted{collection="docs"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestHistogramBucketsAreCumulative(t *testing.T) {
	m := newMetrics()
	m.observe("r", 200, 0)
	m.observe("r", 200, 1e9) // 1s
	h := m.latency["r"]
	if h.total != 2 || h.counts[0] != 1 {
		t.Fatalf("unexpected histogram %+v", h)
	}
}

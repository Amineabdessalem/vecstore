package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// latencyBuckets are the histogram upper bounds in seconds.
var latencyBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}

type routeKey struct {
	route  string
	status int
}

type histogram struct {
	counts []uint64 // per bucket, not cumulative
	sum    float64
	total  uint64
}

// metrics records request counts and latencies by route pattern, and renders
// them with collection gauges in the Prometheus text exposition format.
type metrics struct {
	mu       sync.Mutex
	requests map[routeKey]uint64
	latency  map[string]*histogram
}

func newMetrics() *metrics {
	return &metrics{requests: map[routeKey]uint64{}, latency: map[string]*histogram{}}
}

func (m *metrics) observe(route string, status int, d time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[routeKey{route, status}]++
	h := m.latency[route]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(latencyBuckets))}
		m.latency[route] = h
	}
	sec := d.Seconds()
	for i, b := range latencyBuckets {
		if sec <= b {
			h.counts[i]++
			break
		}
	}
	h.sum += sec
	h.total++
}

func quote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	m := s.metrics
	m.mu.Lock()
	keys := make([]routeKey, 0, len(m.requests))
	for k := range m.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].status < keys[j].status
	})
	b.WriteString("# HELP vecstore_http_requests_total HTTP requests by route and status.\n# TYPE vecstore_http_requests_total counter\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "vecstore_http_requests_total{route=\"%s\",status=\"%d\"} %d\n", quote(k.route), k.status, m.requests[k])
	}
	routes := make([]string, 0, len(m.latency))
	for r := range m.latency {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	b.WriteString("# HELP vecstore_http_request_duration_seconds Request latency by route.\n# TYPE vecstore_http_request_duration_seconds histogram\n")
	for _, route := range routes {
		h, q := m.latency[route], quote(route)
		var cum uint64
		for i, ub := range latencyBuckets {
			cum += h.counts[i]
			fmt.Fprintf(&b, "vecstore_http_request_duration_seconds_bucket{route=\"%s\",le=\"%g\"} %d\n", q, ub, cum)
		}
		fmt.Fprintf(&b, "vecstore_http_request_duration_seconds_bucket{route=\"%s\",le=\"+Inf\"} %d\n", q, h.total)
		fmt.Fprintf(&b, "vecstore_http_request_duration_seconds_sum{route=\"%s\"} %g\n", q, h.sum)
		fmt.Fprintf(&b, "vecstore_http_request_duration_seconds_count{route=\"%s\"} %d\n", q, h.total)
	}
	m.mu.Unlock()

	infos := s.store.List()
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	b.WriteString("# HELP vecstore_collection_vectors Live vectors per collection.\n# TYPE vecstore_collection_vectors gauge\n")
	for _, c := range infos {
		fmt.Fprintf(&b, "vecstore_collection_vectors{collection=\"%s\"} %d\n", quote(c.Name), c.Count)
	}
	b.WriteString("# HELP vecstore_collection_deleted Soft-deleted vectors awaiting compaction.\n# TYPE vecstore_collection_deleted gauge\n")
	for _, c := range infos {
		fmt.Fprintf(&b, "vecstore_collection_deleted{collection=\"%s\"} %d\n", quote(c.Name), c.Deleted)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

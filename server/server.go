// Package server exposes a store over a JSON HTTP API.
package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Amineabdessalem/vecstore/index"
	"github.com/Amineabdessalem/vecstore/store"
)

// maxBody caps request bodies (32 MiB is ~8M float32 values).
const maxBody = 32 << 20

// Server wires HTTP routes to a store.
type Server struct {
	store *store.Store
	log   *slog.Logger
	mux   *http.ServeMux

	metrics *metrics
}

// New returns a server for s.
func New(s *store.Store, log *slog.Logger) *Server {
	srv := &Server{store: s, log: log, mux: http.NewServeMux(), metrics: newMetrics()}
	srv.routes()
	return srv
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /metrics", s.serveMetrics)
	s.mux.HandleFunc("GET /collections", s.listCollections)
	s.mux.HandleFunc("POST /collections", s.createCollection)
	s.mux.HandleFunc("GET /collections/{name}", s.getCollection)
	s.mux.HandleFunc("DELETE /collections/{name}", s.dropCollection)
	s.mux.HandleFunc("POST /collections/{name}/compact", s.compact)
	s.mux.HandleFunc("PUT /collections/{name}/vectors", s.upsert)
	s.mux.HandleFunc("GET /collections/{name}/vectors/{id}", s.getVector)
	s.mux.HandleFunc("DELETE /collections/{name}/vectors/{id}", s.deleteVector)
	s.mux.HandleFunc("POST /collections/{name}/search", s.search)
}

// ServeHTTP implements http.Handler with request logging.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	s.mux.ServeHTTP(rec, r)
	// r.Pattern is set by the mux, so metrics are labelled by route, not by raw path.
	s.metrics.observe(r.Pattern, rec.status, time.Since(start))
	s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
}

type createRequest struct {
	Name   string       `json:"name"`
	Dim    int          `json:"dim"`
	Metric index.Metric `json:"metric"`
}

func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"collections": s.store.List()})
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Metric == "" {
		req.Metric = index.Cosine
	}
	c, err := s.store.Create(req.Name, req.Dim, req.Metric)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c.Info())
}

func (s *Server) collection(w http.ResponseWriter, r *http.Request) (*store.Collection, bool) {
	c, err := s.store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return nil, false
	}
	return c, true
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	if c, ok := s.collection(w, r); ok {
		writeJSON(w, http.StatusOK, c.Info())
	}
}

func (s *Server) dropCollection(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Drop(r.PathValue("name")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) compact(w http.ResponseWriter, r *http.Request) {
	if c, ok := s.collection(w, r); ok {
		writeJSON(w, http.StatusOK, map[string]int{"removed": c.Compact(), "count": c.Info().Count})
	}
}

func (s *Server) upsert(w http.ResponseWriter, r *http.Request) {
	c, ok := s.collection(w, r)
	if !ok {
		return
	}
	var req struct {
		Vectors []store.Record `json:"vectors"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := c.Upsert(req.Vectors); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"upserted": len(req.Vectors), "count": c.Info().Count})
}

func (s *Server) getVector(w http.ResponseWriter, r *http.Request) {
	c, ok := s.collection(w, r)
	if !ok {
		return
	}
	rec, found := c.Get(r.PathValue("id"))
	if !found {
		writeJSON(w, http.StatusNotFound, errorBody("vector not found"))
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) deleteVector(w http.ResponseWriter, r *http.Request) {
	c, ok := s.collection(w, r)
	if !ok {
		return
	}
	if !c.Delete(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, errorBody("vector not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type searchRequest struct {
	Vector []float32    `json:"vector"`
	K      int          `json:"k"`
	Ef     int          `json:"ef"`
	Filter store.Filter `json:"filter"`
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	c, ok := s.collection(w, r)
	if !ok {
		return
	}
	var req searchRequest
	if !decode(w, r, &req) {
		return
	}
	if req.K > 1000 {
		writeJSON(w, http.StatusBadRequest, errorBody("k must be at most 1000"))
		return
	}
	hits, err := c.Search(req.Vector, store.SearchOptions{K: req.K, Ef: req.Ef, Filter: req.Filter})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid JSON: "+err.Error()))
		return false
	}
	return true
}

func errorBody(msg string) map[string]string { return map[string]string{"error": msg} }

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrExists):
		status = http.StatusConflict
	}
	writeJSON(w, status, errorBody(err.Error()))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Package client is a typed Go client for the vecstore HTTP API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Amineabdessalem/vecstore/index"
	"github.com/Amineabdessalem/vecstore/store"
)

// ErrNotFound is returned when the collection or vector does not exist.
var ErrNotFound = errors.New("vecstore: not found")

// APIError is any non-2xx response other than 404.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("vecstore: %d: %s", e.Status, e.Message) }

// Client talks to one vecstore server.
type Client struct {
	base   string
	http   *http.Client
	apiKey string
}

// WithAPIKey returns a copy of c that sends key as a bearer token.
func (c *Client) WithAPIKey(key string) *Client {
	cp := *c
	cp.apiKey = key
	return &cp
}

// New returns a client for a server such as "http://localhost:8080".
// A nil httpClient uses http.DefaultClient.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// SearchRequest mirrors the server's search body; zero K and Ef use server defaults.
type SearchRequest struct {
	Vector []float32    `json:"vector"`
	K      int          `json:"k,omitempty"`
	Ef     int          `json:"ef,omitempty"`
	Filter store.Filter `json:"filter,omitempty"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return &APIError{Status: resp.StatusCode, Message: e.Error}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func col(name string) string { return "/collections/" + url.PathEscape(name) }

// CreateCollection creates a collection; an empty metric means cosine.
func (c *Client) CreateCollection(ctx context.Context, name string, dim int, metric index.Metric) (store.Info, error) {
	var info store.Info
	body := map[string]any{"name": name, "dim": dim}
	if metric != "" {
		body["metric"] = metric
	}
	err := c.do(ctx, http.MethodPost, "/collections", body, &info)
	return info, err
}

// Collection returns a collection's info.
func (c *Client) Collection(ctx context.Context, name string) (store.Info, error) {
	var info store.Info
	err := c.do(ctx, http.MethodGet, col(name), nil, &info)
	return info, err
}

// Collections lists all collections.
func (c *Client) Collections(ctx context.Context) ([]store.Info, error) {
	var out struct {
		Collections []store.Info `json:"collections"`
	}
	err := c.do(ctx, http.MethodGet, "/collections", nil, &out)
	return out.Collections, err
}

// DropCollection deletes a collection and its vectors.
func (c *Client) DropCollection(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, col(name), nil, nil)
}

// Upsert inserts or replaces records and returns the collection's new size.
func (c *Client) Upsert(ctx context.Context, name string, recs []store.Record) (int, error) {
	var out struct {
		Count int `json:"count"`
	}
	err := c.do(ctx, http.MethodPut, col(name)+"/vectors", map[string]any{"vectors": recs}, &out)
	return out.Count, err
}

// Get fetches one record by id.
func (c *Client) Get(ctx context.Context, name, id string) (store.Record, error) {
	var rec store.Record
	err := c.do(ctx, http.MethodGet, col(name)+"/vectors/"+url.PathEscape(id), nil, &rec)
	return rec, err
}

// Delete removes one record by id.
func (c *Client) Delete(ctx context.Context, name, id string) error {
	return c.do(ctx, http.MethodDelete, col(name)+"/vectors/"+url.PathEscape(id), nil, nil)
}

// Search returns the nearest records to req.Vector.
func (c *Client) Search(ctx context.Context, name string, req SearchRequest) ([]store.Hit, error) {
	var out struct {
		Hits []store.Hit `json:"hits"`
	}
	err := c.do(ctx, http.MethodPost, col(name)+"/search", req, &out)
	return out.Hits, err
}

// Compact rebuilds a collection's graph without deleted nodes and returns how many were removed.
func (c *Client) Compact(ctx context.Context, name string) (int, error) {
	var out struct {
		Removed int `json:"removed"`
	}
	err := c.do(ctx, http.MethodPost, col(name)+"/compact", nil, &out)
	return out.Removed, err
}

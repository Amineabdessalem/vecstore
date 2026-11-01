package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/Amineabdessalem/vecstore/index"
	"github.com/Amineabdessalem/vecstore/server"
	"github.com/Amineabdessalem/vecstore/store"
)

func newClient(t *testing.T) *Client {
	t.Helper()
	srv := server.New(store.New(index.DefaultHNSWConfig()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return New(ts.URL+"/", ts.Client())
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	if _, err := c.CreateCollection(ctx, "docs", 2, index.Euclidean); err != nil {
		t.Fatal(err)
	}
	n, err := c.Upsert(ctx, "docs", []store.Record{
		{ID: "a", Values: []float32{0, 0}, Metadata: store.Metadata{"lang": "en"}},
		{ID: "b", Values: []float32{1, 1}, Metadata: store.Metadata{"lang": "fr"}},
		{ID: "c", Values: []float32{5, 5}, Metadata: store.Metadata{"lang": "en"}},
	})
	if err != nil || n != 3 {
		t.Fatalf("upsert: n=%d err=%v", n, err)
	}
	hits, err := c.Search(ctx, "docs", SearchRequest{Vector: []float32{0.9, 0.9}, K: 2})
	if err != nil || len(hits) != 2 || hits[0].ID != "b" {
		t.Fatalf("search: %+v %v", hits, err)
	}
	hits, err = c.Search(ctx, "docs", SearchRequest{Vector: []float32{0.9, 0.9}, K: 1, Filter: store.Filter{"lang": "en"}})
	if err != nil || len(hits) != 1 || hits[0].ID != "a" {
		t.Fatalf("filtered search: %+v %v", hits, err)
	}
	rec, err := c.Get(ctx, "docs", "c")
	if err != nil || rec.Values[0] != 5 {
		t.Fatalf("get: %+v %v", rec, err)
	}
	if err := c.Delete(ctx, "docs", "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "docs", "c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	info, err := c.Collection(ctx, "docs")
	if err != nil || info.Count != 2 {
		t.Fatalf("info: %+v %v", info, err)
	}
	if _, err := c.Compact(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	cols, err := c.Collections(ctx)
	if err != nil || len(cols) != 1 {
		t.Fatalf("list: %+v %v", cols, err)
	}
	if err := c.DropCollection(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	if _, err := c.Collection(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := c.CreateCollection(ctx, "x", 3, ""); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateCollection(ctx, "x", 3, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 409 {
		t.Fatalf("want 409 APIError, got %v", err)
	}
	if _, err := c.Search(ctx, "x", SearchRequest{Vector: []float32{1}}); !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("want 400 for wrong dim, got %v", err)
	}
}

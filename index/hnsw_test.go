package index

import (
	"fmt"
	"math/rand"
	"testing"
)

func randomVectors(n, dim int, seed int64) [][]float32 {
	r := rand.New(rand.NewSource(seed))
	out := make([][]float32, n)
	for i := range out {
		v := make([]float32, dim)
		for j := range v {
			v[j] = float32(r.NormFloat64())
		}
		out[i] = v
	}
	return out
}

func recallAt(t *testing.T, metric Metric, n, dim, k int) float64 {
	t.Helper()
	data := randomVectors(n, dim, 1)
	queries := randomVectors(50, dim, 2)
	flat, _ := NewFlat(dim, metric)
	h, _ := NewHNSW(dim, metric, DefaultHNSWConfig())
	for i, v := range data {
		id := fmt.Sprint(i)
		_ = flat.Add(id, v)
		if err := h.Add(id, v); err != nil {
			t.Fatal(err)
		}
	}
	hits, total := 0, 0
	for _, q := range queries {
		want, _ := flat.Search(q, k)
		got, _ := h.Search(q, k)
		truth := map[string]bool{}
		for _, r := range want {
			truth[r.ID] = true
		}
		for _, r := range got {
			if truth[r.ID] {
				hits++
			}
		}
		total += len(want)
	}
	return float64(hits) / float64(total)
}

func TestHNSWRecall(t *testing.T) {
	for _, m := range []Metric{Cosine, Euclidean} {
		r := recallAt(t, m, 2000, 32, 10)
		t.Logf("%s recall@10 = %.3f", m, r)
		if r < 0.95 {
			t.Errorf("%s recall@10 = %.3f, want >= 0.95", m, r)
		}
	}
}

func TestHNSWResultsSortedAndUnique(t *testing.T) {
	h, _ := NewHNSW(8, Euclidean, DefaultHNSWConfig())
	for i, v := range randomVectors(300, 8, 3) {
		_ = h.Add(fmt.Sprint(i), v)
	}
	res, err := h.Search(randomVectors(1, 8, 4)[0], 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 20 {
		t.Fatalf("got %d results", len(res))
	}
	seen := map[string]bool{}
	for i, r := range res {
		if seen[r.ID] {
			t.Errorf("duplicate id %s", r.ID)
		}
		seen[r.ID] = true
		if i > 0 && r.Distance < res[i-1].Distance {
			t.Errorf("results not sorted at %d", i)
		}
	}
}

func TestHNSWDeleteAndReplace(t *testing.T) {
	h, _ := NewHNSW(2, Euclidean, DefaultHNSWConfig())
	_ = h.Add("a", []float32{0, 0})
	_ = h.Add("b", []float32{10, 10})
	if !h.Delete("a") || h.Delete("a") || h.Len() != 1 {
		t.Fatalf("delete bookkeeping wrong, len=%d", h.Len())
	}
	res, _ := h.Search([]float32{0, 0}, 1)
	if len(res) != 1 || res[0].ID != "b" {
		t.Errorf("deleted node returned: %+v", res)
	}
	// Re-adding moves "b" next to the query.
	_ = h.Add("b", []float32{0.1, 0})
	res, _ = h.Search([]float32{0, 0}, 5)
	if len(res) != 1 || res[0].ID != "b" || res[0].Distance > 0.2 {
		t.Errorf("replace failed: %+v", res)
	}
}

func TestHNSWEmpty(t *testing.T) {
	h, _ := NewHNSW(3, Cosine, DefaultHNSWConfig())
	if res, err := h.Search([]float32{1, 0, 0}, 3); err != nil || len(res) != 0 {
		t.Errorf("empty search: %v %v", res, err)
	}
}

func BenchmarkHNSWSearch(b *testing.B) {
	h, _ := NewHNSW(64, Cosine, DefaultHNSWConfig())
	for i, v := range randomVectors(10000, 64, 1) {
		_ = h.Add(fmt.Sprint(i), v)
	}
	qs := randomVectors(100, 64, 2)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.Search(qs[i%len(qs)], 10)
	}
}

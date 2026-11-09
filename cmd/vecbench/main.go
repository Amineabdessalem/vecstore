// Command vecbench measures HNSW recall@k and query latency against exact
// search on synthetic clustered data, sweeping the ef search parameter.
//
//	go run ./cmd/vecbench -n 20000 -dim 128 -ef 16,32,64,128,256
package main

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Amineabdessalem/vecstore/index"
)

// clustered draws n vectors around `clusters` random centres, which is closer
// to real embeddings than uniform noise and makes the graph search harder.
func clustered(rng *rand.Rand, n, dim, clusters int) [][]float32 {
	centres := make([][]float32, clusters)
	for i := range centres {
		centres[i] = make([]float32, dim)
		for j := range centres[i] {
			centres[i][j] = float32(rng.NormFloat64())
		}
	}
	out := make([][]float32, n)
	for i := range out {
		c := centres[rng.IntN(clusters)]
		v := make([]float32, dim)
		for j := range v {
			v[j] = c[j] + 0.35*float32(rng.NormFloat64())
		}
		out[i] = v
	}
	return out
}

func parseInts(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(s, ",") {
		v, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("bad ef value %q", f)
		}
		out = append(out, v)
	}
	sort.Ints(out)
	return out, nil
}

func recall(got []index.Result, truth map[string]bool) float64 {
	hit := 0
	for _, r := range got {
		if truth[r.ID] {
			hit++
		}
	}
	return float64(hit) / float64(len(truth))
}

func main() {
	n := flag.Int("n", 20000, "vectors to index")
	dim := flag.Int("dim", 64, "dimensions")
	queries := flag.Int("queries", 200, "queries to run")
	k := flag.Int("k", 10, "neighbours per query")
	clusters := flag.Int("clusters", 50, "clusters in the synthetic data")
	m := flag.Int("m", 16, "HNSW M")
	efc := flag.Int("ef-construction", 200, "HNSW efConstruction")
	efList := flag.String("ef", "10,20,40,80,160", "comma-separated ef values to sweep")
	metric := flag.String("metric", "cosine", "cosine, euclidean or dot")
	seed := flag.Uint64("seed", 1, "random seed")
	flag.Parse()

	efs, err := parseInts(*efList)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	rng := rand.New(rand.NewPCG(*seed, 0))
	data := clustered(rng, *n+*queries, *dim, *clusters)
	base, qs := data[:*n], data[*n:]

	flat, err := index.NewFlat(*dim, index.Metric(*metric))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg := index.HNSWConfig{M: *m, EfConstruction: *efc, EfSearch: efs[0], Seed: int64(*seed)}
	hnsw, err := index.NewHNSW(*dim, index.Metric(*metric), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	start := time.Now()
	for i, v := range base {
		id := strconv.Itoa(i)
		_ = flat.Add(id, v)
		if err := hnsw.Add(id, v); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	build := time.Since(start)

	truth := make([]map[string]bool, len(qs))
	start = time.Now()
	for i, q := range qs {
		res, _ := flat.Search(q, *k)
		truth[i] = make(map[string]bool, len(res))
		for _, r := range res {
			truth[i][r.ID] = true
		}
	}
	exact := time.Since(start) / time.Duration(len(qs))

	fmt.Printf("%d vectors x %d dims (%s), %d queries, k=%d, M=%d, efConstruction=%d\n",
		*n, *dim, *metric, len(qs), *k, *m, *efc)
	fmt.Printf("build %.2fs, exact search %v/query\n\n", build.Seconds(), exact.Round(time.Microsecond))
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "ef\trecall@k\tlatency/query\tspeed-up vs exact\t")
	for _, ef := range efs {
		var sum float64
		start := time.Now()
		for i, q := range qs {
			res, _ := hnsw.SearchEf(q, *k, ef)
			sum += recall(res, truth[i])
		}
		per := time.Since(start) / time.Duration(len(qs))
		fmt.Fprintf(tw, "%d\t%.3f\t%v\t%.1fx\t\n", ef, sum/float64(len(qs)), per.Round(time.Microsecond),
			float64(exact)/float64(max(per, 1)))
	}
	tw.Flush()
}

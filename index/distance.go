// Package index implements vector similarity search: an exact flat index and
// an approximate HNSW graph index.
package index

import (
	"fmt"
	"math"
)

// Metric names a distance function. Smaller distances mean more similar.
type Metric string

const (
	Cosine    Metric = "cosine"
	Euclidean Metric = "euclidean"
	Dot       Metric = "dot"
)

// DistanceFunc returns the distance between two equal-length vectors.
type DistanceFunc func(a, b []float32) float32

// Func returns the distance function for m.
func (m Metric) Func() (DistanceFunc, error) {
	switch m {
	case Cosine:
		return cosineDistance, nil
	case Euclidean:
		return euclideanDistance, nil
	case Dot:
		return negativeDot, nil
	default:
		return nil, fmt.Errorf("unknown metric %q", m)
	}
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// cosineDistance assumes neither vector is zero; Normalize is applied on
// insert and query so this reduces to 1 - dot.
func cosineDistance(a, b []float32) float32 { return 1 - dot(a, b) }

func euclideanDistance(a, b []float32) float32 {
	var s float32
	for i := range a {
		d := a[i] - b[i]
		s += d * d
	}
	return float32(math.Sqrt(float64(s)))
}

func negativeDot(a, b []float32) float32 { return -dot(a, b) }

// Normalize returns a unit-length copy of v. A zero vector is returned as is.
func Normalize(v []float32) []float32 {
	n := float32(math.Sqrt(float64(dot(v, v))))
	out := make([]float32, len(v))
	if n == 0 {
		copy(out, v)
		return out
	}
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

package index

import (
	"errors"
	"math"
	"testing"
)

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

func TestMetrics(t *testing.T) {
	a, b := []float32{1, 0}, []float32{0, 1}
	cos, _ := Cosine.Func()
	euc, _ := Euclidean.Func()
	dt, _ := Dot.Func()
	if !approx(cos(a, b), 1) || !approx(cos(a, a), 0) {
		t.Errorf("cosine distance wrong")
	}
	if !approx(euc(a, b), float32(math.Sqrt2)) {
		t.Errorf("euclidean distance wrong: %v", euc(a, b))
	}
	if !approx(dt([]float32{2, 3}, []float32{4, 5}), -23) {
		t.Errorf("dot distance wrong")
	}
	if _, err := Metric("manhattan").Func(); err == nil {
		t.Errorf("expected error for unknown metric")
	}
}

func TestNormalize(t *testing.T) {
	v := Normalize([]float32{3, 4})
	if !approx(v[0], 0.6) || !approx(v[1], 0.8) {
		t.Errorf("got %v", v)
	}
	if z := Normalize([]float32{0, 0}); z[0] != 0 || z[1] != 0 {
		t.Errorf("zero vector changed: %v", z)
	}
}

func TestFlatSearch(t *testing.T) {
	f, _ := NewFlat(2, Euclidean)
	for id, v := range map[string][]float32{"a": {0, 0}, "b": {1, 0}, "c": {5, 5}} {
		if err := f.Add(id, v); err != nil {
			t.Fatal(err)
		}
	}
	res, err := f.Search([]float32{0.9, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].ID != "b" || res[1].ID != "a" {
		t.Errorf("unexpected results %+v", res)
	}
	if !f.Delete("b") || f.Delete("b") || f.Len() != 2 {
		t.Errorf("delete bookkeeping wrong")
	}
}

func TestFlatValidation(t *testing.T) {
	f, _ := NewFlat(3, Cosine)
	if err := f.Add("x", []float32{1, 2}); !errors.Is(err, ErrDimension) {
		t.Errorf("want ErrDimension, got %v", err)
	}
	if err := f.Add("", []float32{1, 2, 3}); !errors.Is(err, ErrEmptyID) {
		t.Errorf("want ErrEmptyID, got %v", err)
	}
	if _, err := f.Search([]float32{1}, 1); !errors.Is(err, ErrDimension) {
		t.Errorf("want ErrDimension on search, got %v", err)
	}
}

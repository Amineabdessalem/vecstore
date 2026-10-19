package store

import "testing"

func TestFilterMatch(t *testing.T) {
	m := Metadata{"lang": "go", "year": 2023.0, "stars": 120, "tags": "db"}
	cases := []struct {
		name string
		f    Filter
		want bool
	}{
		{"equality", Filter{"lang": "go"}, true},
		{"equality miss", Filter{"lang": "rust"}, false},
		{"missing key", Filter{"license": "mit"}, false},
		{"int vs float", Filter{"year": 2023}, true},
		{"range inside", Filter{"year": map[string]any{"gte": 2020.0, "lt": 2025.0}}, true},
		{"range outside", Filter{"year": map[string]any{"gt": 2023.0}}, false},
		{"int metadata", Filter{"stars": map[string]any{"gte": 100.0}}, true},
		{"in", Filter{"lang": map[string]any{"in": []any{"rust", "go"}}}, true},
		{"not in", Filter{"lang": map[string]any{"in": []any{"rust"}}}, false},
		{"ne", Filter{"lang": map[string]any{"ne": "python"}}, true},
		{"range on string", Filter{"lang": map[string]any{"gt": 1.0}}, false},
		{"all conditions", Filter{"lang": "go", "year": map[string]any{"lt": 2000.0}}, false},
	}
	for _, tc := range cases {
		if got := tc.f.match(m); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFilterValidate(t *testing.T) {
	if err := (Filter{"year": map[string]any{"gtee": 1.0}}).Validate(); err == nil {
		t.Error("expected unknown operator error")
	}
	if err := (Filter{"year": map[string]any{"gte": 1.0}, "lang": "go"}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

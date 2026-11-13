package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEndpointsExistInPinnedSpecs verifies every endpoint the client calls
// against each pinned Forgejo swagger spec (IMPLEMENTATION §9.1, B-8).
func TestEndpointsExistInPinnedSpecs(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "test", "fixtures", "swagger", "forgejo-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no pinned swagger specs found: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var spec struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, e := range Endpoints() {
			ops, ok := spec.Paths[e.Path]
			if !ok {
				t.Errorf("%s: path %s not in spec", filepath.Base(f), e.Path)
				continue
			}
			if _, ok := ops[strings.ToLower(e.Method)]; !ok {
				t.Errorf("%s: %s %s not in spec", filepath.Base(f), e.Method, e.Path)
			}
		}
	}
}

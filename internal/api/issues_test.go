package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestIssuePagination(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("type") != "issues" {
			t.Error("missing issue type")
		}
		if r.URL.Query().Get("mentioned_by") != "alice" {
			t.Error("missing mention filter")
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `[{"id":999,"number":3},{"number":4}]`)
			return
		}
		q := r.URL.Query()
		q.Set("page", "2")
		w.Header().Set("Link", "<"+r.URL.Path+"?"+q.Encode()+">; rel=\"next\"")
		fmt.Fprint(w, `[{"number":1},{"number":2}]`)
	}))
	defer srv.Close()
	c, err := New(Options{BaseURL: srv.URL + "/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := c.ListIssues(t.Context(), "org", "repo", IssueSearch{MentionedBy: "alice"}, 3)
	if err != nil || len(items) != 3 || items[2].Number != 3 || requests != 2 {
		t.Fatalf("items %+v requests %d error %v", items, requests, err)
	}
}

func TestIssueFiltersExistInPinnedSpecs(t *testing.T) {
	files, _ := filepath.Glob("../../test/fixtures/swagger/forgejo-*.json")
	if len(files) != 2 {
		t.Fatal("expected both pinned specs")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var spec struct {
			Paths map[string]map[string]struct{ Parameters []struct{ Name, In string } }
		}
		if err = json.Unmarshal(data, &spec); err != nil {
			t.Fatal(err)
		}
		params := url.Values{}
		for _, p := range spec.Paths["/repos/{owner}/{repo}/issues"]["get"].Parameters {
			if p.In == "query" {
				params.Set(p.Name, "yes")
			}
		}
		for _, name := range []string{"type", "state", "labels", "milestones", "created_by", "assigned_by", "mentioned_by", "q", "limit", "page"} {
			if params.Get(name) == "" {
				t.Errorf("%s: missing %s", file, name)
			}
		}
	}
}

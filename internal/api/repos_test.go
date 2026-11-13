package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
)

func repoServer(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var queries []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/repos/search":
			queries = append(queries, r.URL.RawQuery)
			page := r.URL.Query().Get("page")
			w.Header().Set("X-Total-Count", "5")
			switch page {
			case "", "1":
				w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/repos/search?page=2&%s>; rel="next"`, srv.URL, r.URL.RawQuery))
				_, _ = io.WriteString(w, `{"ok":true,"data":[{"full_name":"a/1"},{"full_name":"a/2"},{"full_name":"a/3"}]}`)
			default:
				_, _ = io.WriteString(w, `{"ok":true,"data":[{"full_name":"a/4"},{"full_name":"a/5"}]}`)
			}
		case "/api/v1/repos/a/1":
			_, _ = io.WriteString(w, `{"full_name":"a/1","private":true,"parent":{"full_name":"up/1"},"open_pr_counter":2}`)
		case "/api/v1/users/org":
			_, _ = io.WriteString(w, `{"id":42,"login":"org"}`)
		case "/api/v1/repos/bad/json":
			_, _ = io.WriteString(w, `{`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"not found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := New(Options{BaseURL: srv.URL + "/api/v1", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	return c, &queries
}

func TestSearchRepositoriesPagesAndLimit(t *testing.T) {
	c, queries := repoServer(t)
	yes, no := true, false
	repos, total, err := c.SearchRepositories(context.Background(),
		RepoSearch{OwnerID: 9, Private: &yes, Archived: &no, Mode: "fork", Sort: "updated", Order: "desc"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(repos) != 4 || repos[3].FullName != "a/4" {
		t.Errorf("total %d, repos %+v", total, repos)
	}
	q := (*queries)[0]
	for _, want := range []string{"uid=9", "exclusive=true", "is_private=true", "archived=false", "mode=fork", "sort=updated", "order=desc", "limit=4"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q missing %q", q, want)
		}
	}

	all, _, err := c.SearchRepositories(context.Background(), RepoSearch{}, 0)
	if err != nil || len(all) != 5 {
		t.Errorf("all: %d repos, %v", len(all), err)
	}
	if q := (*queries)[len(*queries)-1]; strings.Contains(q, "uid=") || strings.Contains(q, "order=") {
		t.Errorf("unset filters were sent: %q", q)
	}
}

func TestGetRepositoryAndUser(t *testing.T) {
	c, _ := repoServer(t)
	ctx := context.Background()
	r, err := c.GetRepository(ctx, "a", "1")
	if err != nil || !r.Private || r.Parent == nil || r.Parent.FullName != "up/1" || r.OpenPulls != 2 {
		t.Errorf("GetRepository = %+v, %v", r, err)
	}
	if _, err := c.GetRepository(ctx, "a", "missing"); fjoerr.KindOf(err) != fjoerr.KindNotFound {
		t.Errorf("missing repo: %v", err)
	}
	if _, err := c.GetRepository(ctx, "bad", "json"); err == nil {
		t.Error("invalid JSON should fail")
	}
	u, err := c.GetUser(ctx, "org")
	if err != nil || u.ID != 42 {
		t.Errorf("GetUser = %+v, %v", u, err)
	}
	if _, err := c.GetUser(ctx, "ghost"); fjoerr.KindOf(err) != fjoerr.KindNotFound {
		t.Errorf("missing user: %v", err)
	}
}

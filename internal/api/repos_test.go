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

type mutation struct {
	method, path, body string
}

func mutationServer(t *testing.T) (*Client, *[]mutation) {
	t.Helper()
	var got []mutation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, mutation{r.Method, r.URL.EscapedPath(), string(b)})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			if strings.Contains(string(b), `"name":"exists"`) {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"message":"repository already exists"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"full_name":"alice/demo","html_url":"https://forge/alice/demo"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/org/repos":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"full_name":"org/demo"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/alice/demo":
			_, _ = io.WriteString(w, `{"full_name":"alice/demo","description":"new"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/alice/demo":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/up/project/forks":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"full_name":"alice/project"}`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.EscapedPath(), "/api/v1/repos/alice/project/sync_fork"):
			w.WriteHeader(http.StatusNoContent)
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
	return c, &got
}

func TestCreateRepository(t *testing.T) {
	c, got := mutationServer(t)
	ctx := context.Background()

	private := true
	r, err := c.CreateRepository(ctx, "", CreateRepoOptions{
		Name: "demo", Description: "test", Private: &private, AutoInit: true,
		DefaultBranch: "main", Gitignores: "Go", License: "MIT",
	})
	if err != nil || r.FullName != "alice/demo" {
		t.Fatalf("CreateRepository = %+v, %v", r, err)
	}
	m := (*got)[0]
	if m.method != http.MethodPost || m.path != "/api/v1/user/repos" {
		t.Errorf("request = %+v", m)
	}
	for _, want := range []string{`"name":"demo"`, `"private":true`, `"auto_init":true`, `"default_branch":"main"`, `"gitignores":"Go"`, `"license":"MIT"`} {
		if !strings.Contains(m.body, want) {
			t.Errorf("body %s missing %s", m.body, want)
		}
	}

	if _, err := c.CreateRepository(ctx, "org", CreateRepoOptions{Name: "demo"}); err != nil {
		t.Fatalf("org create: %v", err)
	}
	if m := (*got)[1]; m.path != "/api/v1/orgs/org/repos" {
		t.Errorf("org path = %q", m.path)
	}

	if _, err := c.CreateRepository(ctx, "", CreateRepoOptions{Name: "exists"}); fjoerr.KindOf(err) != fjoerr.KindConflict {
		t.Errorf("conflict kind = %q, err = %v", fjoerr.KindOf(err), err)
	}
}

func TestEditRepository(t *testing.T) {
	c, got := mutationServer(t)
	desc, website, branch := "new", "https://example.com", "develop"
	private, archived := false, true

	r, err := c.EditRepository(context.Background(), "alice", "demo", EditRepoOptions{
		Description: &desc, Website: &website, Private: &private,
		Archived: &archived, DefaultBranch: &branch,
	})
	if err != nil || r.Description != "new" {
		t.Fatalf("EditRepository = %+v, %v", r, err)
	}
	m := (*got)[0]
	if m.method != http.MethodPatch || m.path != "/api/v1/repos/alice/demo" {
		t.Errorf("request = %+v", m)
	}
	for _, want := range []string{`"description":"new"`, `"website":"https://example.com"`, `"private":false`, `"archived":true`, `"default_branch":"develop"`} {
		if !strings.Contains(m.body, want) {
			t.Errorf("body %s missing %s", m.body, want)
		}
	}

	if _, err := c.EditRepository(context.Background(), "alice", "demo", EditRepoOptions{}); err != nil {
		t.Fatalf("empty edit: %v", err)
	}
	if got := (*got)[1].body; got != "{}" {
		t.Errorf("empty edit body = %q", got)
	}
}

func TestDeleteForkAndSyncRepository(t *testing.T) {
	c, got := mutationServer(t)
	ctx := context.Background()

	if err := c.DeleteRepository(ctx, "alice", "demo"); err != nil {
		t.Fatalf("DeleteRepository: %v", err)
	}
	if m := (*got)[0]; m.method != http.MethodDelete || m.path != "/api/v1/repos/alice/demo" {
		t.Errorf("delete request = %+v", m)
	}

	fork, err := c.ForkRepository(ctx, "up", "project", ForkRepoOptions{Name: "project", Organization: "org"})
	if err != nil || fork.FullName != "alice/project" {
		t.Fatalf("ForkRepository = %+v, %v", fork, err)
	}
	m := (*got)[1]
	if m.path != "/api/v1/repos/up/project/forks" || !strings.Contains(m.body, `"organization":"org"`) {
		t.Errorf("fork request = %+v", m)
	}

	if err := c.SyncFork(ctx, "alice", "project", ""); err != nil {
		t.Fatalf("SyncFork default: %v", err)
	}
	if m := (*got)[2]; m.path != "/api/v1/repos/alice/project/sync_fork" {
		t.Errorf("sync path = %q", m.path)
	}
	if err := c.SyncFork(ctx, "alice", "project", "feature/x"); err != nil {
		t.Fatalf("SyncFork branch: %v", err)
	}
	if m := (*got)[3]; m.path != "/api/v1/repos/alice/project/sync_fork/feature%2Fx" {
		t.Errorf("sync branch path = %q", m.path)
	}
}

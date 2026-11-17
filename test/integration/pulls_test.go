//go:build integration

package integration_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/fjoerr"
)

func TestReadPullRequestsAgainstForgejo(t *testing.T) {
	inst := configured(t)
	ctx := t.Context()
	c, err := api.New(api.Options{BaseURL: apiURL(inst.url), Token: inst.token})
	if err != nil {
		t.Fatal(err)
	}
	me, err := c.CurrentUser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("fjo-pulls-%d", time.Now().UnixNano())
	repo, err := c.CreateRepository(ctx, "", api.CreateRepoOptions{Name: name, AutoInit: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.DeleteRepository(context.Background(), me.Login, name) })
	full := me.Login + "/" + name
	path := "repos/" + full
	post := func(endpoint string, in, out any) {
		t.Helper()
		if _, err := c.JSON(ctx, http.MethodPost, path+endpoint, in, out); err != nil {
			t.Fatal(err)
		}
	}
	const branch = "feature-read"
	const file = "feature.txt"
	post("/branches", map[string]string{"new_branch_name": branch, "old_branch_name": repo.DefaultBranch}, nil)
	post("/contents/"+file, map[string]string{
		"branch": branch, "message": "Add feature file",
		"content": base64.StdEncoding.EncodeToString([]byte("PR content\n")),
	}, nil)
	var pull api.PullRequest
	post("/pulls", map[string]string{
		"base": repo.DefaultBranch, "head": branch, "title": "WIP: New feature", "body": "PR description",
	}, &pull)
	if pull.Number < 1 || pull.Head.SHA == "" {
		t.Fatalf("missing index or head SHA: %+v", pull)
	}
	if _, err := c.CreateIssueComment(ctx, me.Login, name, pull.Number, "PR discussion"); err != nil {
		t.Fatal(err)
	}
	post("/statuses/"+pull.Head.SHA, map[string]string{"context": "integration", "state": "pending"}, nil)

	r, env := envRunner(t, inst)
	invoke := func(code fjoerr.ExitCode, want string, args ...string) {
		t.Helper()
		res := r.run(env, "", append(append([]string{"pr"}, args...), "-R", full)...)
		if res.code != code || !strings.Contains(res.stdout, want) {
			t.Fatalf("pr %v: exit %d, stdout %q, stderr %q", args, res.code, res.stdout, res.stderr)
		}
	}
	number := strconv.Itoa(pull.Number)
	invoke(0, number+"\n", "list", "--base", repo.DefaultBranch, "--head", branch, "--draft", "--author", me.Login,
		"--json", "number", "--jq", ".[0].number")
	invoke(0, "PR discussion", "view", pull.HTMLURL, "--comments")
	invoke(0, "PR description", "view", branch, "--json", "body", "--jq", ".body")
	invoke(0, file+"\n", "diff", number, "--name-only")
	invoke(0, "+PR content\n", "diff", number, "--patch", "--color=never")
	invoke(fjoerr.ExitPending, "pending\n", "checks", number, "--json", "bucket", "--jq", ".[0].bucket")
	post("/statuses/"+pull.Head.SHA, map[string]string{"context": "integration", "state": "success"}, nil)
	invoke(0, "pass\n", "checks", number, "--json", "bucket", "--jq", ".[0].bucket")
}

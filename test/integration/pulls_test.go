//go:build integration

package integration_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
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
		if err := c.JSON(ctx, http.MethodPost, path+endpoint, in, out); err != nil {
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

func TestWritePullRequestsAgainstForgejo(t *testing.T) {
	inst := configured(t)
	ctx := t.Context()
	client, err := api.New(api.Options{BaseURL: apiURL(inst.url), Token: inst.token})
	if err != nil {
		t.Fatal(err)
	}
	me, err := client.CurrentUser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("fjo-pr-write-%d", time.Now().UnixNano())
	repo, err := client.CreateRepository(ctx, "", api.CreateRepoOptions{Name: name, AutoInit: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.DeleteRepository(context.Background(), me.Login, name); err != nil {
			t.Errorf("clean up test repository: %v", err)
		}
	})
	full := me.Login + "/" + name
	path := "repos/" + full
	r, env := envRunner(t, inst)
	invoke := func(code fjoerr.ExitCode, args ...string) result {
		t.Helper()
		res := r.run(env, "", append(append([]string{"pr"}, args...), "-R", full)...)
		if res.code != code {
			t.Fatalf("pr %v: exit %d, stdout %q, stderr %q", args, res.code, res.stdout, res.stderr)
		}
		return res
	}
	request := func(method, endpoint string, in any) {
		t.Helper()
		if err := client.JSON(ctx, method, path+endpoint, in, nil); err != nil {
			t.Fatal(err)
		}
	}
	request(http.MethodPatch, "", map[string]bool{
		"allow_merge_commits": true, "allow_squash_merge": true, "allow_rebase": true,
		"allow_rebase_explicit": true, "allow_fast_forward_only_merge": true,
	})
	create := func(branch string, flags ...string) int {
		t.Helper()
		request(http.MethodPost, "/branches", map[string]string{"new_branch_name": branch, "old_branch_name": repo.DefaultBranch})
		request(http.MethodPost, "/contents/"+strings.ReplaceAll(branch, "/", "-")+".txt", map[string]string{
			"branch": branch, "message": "Add " + branch, "content": base64.StdEncoding.EncodeToString([]byte(branch + "\n")),
		})
		res := invoke(0, append([]string{"create", "--head", branch, "--title", "Feature " + branch}, flags...)...)
		_, number, ok := strings.Cut(strings.TrimSpace(res.stdout), "/pulls/")
		n, err := strconv.Atoi(number)
		if !ok || err != nil || n <= 0 {
			t.Fatalf("unexpected pull request URL: %q", res.stdout)
		}
		return n
	}
	index := create("topic/lifecycle", "--draft", "--body", "Description", "--assignee", "@me")
	n := strconv.Itoa(index)
	duplicate := invoke(fjoerr.ExitConflict, "create", "--head", "topic/lifecycle", "--title", "Duplicate")
	if !strings.Contains(duplicate.stderr, "/pulls/"+n) {
		t.Fatal(duplicate.stderr)
	}
	t.Run("checkout", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git unavailable")
		}
		g := &git.Client{Dir: r.gitDir}
		if _, err := g.Run(t.Context(), "clone", "--", repo.CloneURL, "."); err != nil {
			t.Fatal(err)
		}
		res := r.run(env, "", "pr", "checkout", n, "--branch", "review", "-R", full)
		if res.code != 0 {
			t.Fatalf("checkout: exit %d, stderr %s", res.code, res.stderr)
		}
		branch, err := g.CurrentBranch(t.Context())
		if err != nil || branch != "review" {
			t.Fatalf("current branch = %q, err = %v", branch, err)
		}
		testCreateFromLocalBranch(t, g, r, env, full, me.Login, inst.token, repo.DefaultBranch)
	})
	invoke(0, "ready", n)
	ready, err := client.GetPull(ctx, me.Login, name, index)
	if err != nil || ready.IsDraft() {
		t.Fatalf("expected a ready pull request: %+v, %v", ready, err)
	}
	invoke(0, "ready", n, "--undo")
	draft, err := client.GetPull(ctx, me.Login, name, index)
	if err != nil || !draft.IsDraft() {
		t.Fatalf("expected a draft pull request: %+v, %v", draft, err)
	}
	invoke(0, "edit", n, "--title", "Edited", "--body", "", "--remove-assignee", "@me")
	invoke(0, "comment", n, "--body", "Discussion")
	invoke(0, "close", n, "--comment", "Closing")
	closed, err := client.GetPull(ctx, me.Login, name, index)
	if err != nil || closed.State != "closed" || closed.Title != "Edited" || closed.Body != "" || len(closed.Assignees) != 0 {
		t.Fatalf("unexpected closed pull request: %+v, %v", closed, err)
	}
	invoke(0, "reopen", n)
	invoke(0, "close", n, "--delete-branch")
	if _, err := client.GetBranchSHA(ctx, me.Login, name, "topic/lifecycle"); fjoerr.KindOf(err) != fjoerr.KindNotFound {
		t.Fatalf("closed head branch still exists: %v", err)
	}

	for _, method := range []string{"merge", "squash", "rebase", "rebase-merge", "fast-forward-only"} {
		index := create("test-" + method)
		number := strconv.Itoa(index)
		waitForReadyPull(t, client, me.Login, name, index)
		invoke(0, "merge", number, "--"+method, "--delete-branch")
		merged, err := client.GetPull(ctx, me.Login, name, index)
		if err != nil || !merged.IsMerged() {
			t.Fatalf("expected merged pull request for %s: %+v, %v", method, merged, err)
		}
		if _, err := client.GetBranchSHA(ctx, me.Login, name, "test-"+method); fjoerr.KindOf(err) != fjoerr.KindNotFound {
			t.Fatalf("expected head branch to be deleted: %v", err)
		}
	}
}

func waitForReadyPull(t *testing.T, c *api.Client, owner, repo string, index int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		pull, err := c.GetPull(t.Context(), owner, repo, index)
		if err != nil {
			t.Fatal(err)
		}
		if pull.Mergeable != nil && *pull.Mergeable {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pull request #%d did not become mergeable", index)
}

func testCreateFromLocalBranch(t *testing.T, g *git.Client, r *runner, env map[string]string, repo, user, token, base string) {
	t.Helper()
	ctx := t.Context()
	gitRun := func(args ...string) {
		t.Helper()
		if _, err := g.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	auth := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+token))
	if _, err := g.Run(ctx, "config", "--local", "http.extraHeader", auth); err != nil {
		t.Fatal("could not configure temporary Git authentication")
	}
	gitRun("switch", "-c", "local-feature", base)
	for i, msg := range []string{"First change", "Second change"} {
		name := fmt.Sprintf("change-%d.txt", i)
		if err := os.WriteFile(filepath.Join(g.Dir, name), []byte(msg+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitRun("add", "--", name)
		gitRun("-c", "user.name=FJO Test", "-c", "user.email=fjo@example.org", "-c", "commit.gpgsign=false",
			"commit", "-m", msg, "-m", "Description of "+msg)
	}
	res := r.run(env, "", "pr", "create", "--fill-first", "-R", repo)
	if res.code != fjoerr.ExitNotFound {
		t.Fatalf("missing remote branch: exit %d, stderr %s", res.code, res.stderr)
	}
	res = r.run(env, "", "pr", "create", "--fill-first", "--push", "-R", repo)
	if res.code != 0 {
		t.Fatalf("push and create: exit %d, stderr %s", res.code, res.stderr)
	}
	res = r.run(env, "", "pr", "view", strings.TrimSpace(res.stdout), "--json", "title,body", "--jq", `.title + " / " + .body`)
	if res.code != 0 || res.stdout != "First change / Description of First change\n" {
		t.Fatalf("filled pull request: exit %d, stdout %q, stderr %s", res.code, res.stdout, res.stderr)
	}
}

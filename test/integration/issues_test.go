//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/fjoerr"
)

func TestIssuesAgainstForgejo(t *testing.T) {
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
	name := fmt.Sprintf("fjo-issues-%d", time.Now().UnixNano())
	full := me.Login + "/" + name
	_, err = c.CreateRepository(ctx, "", api.CreateRepoOptions{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.DeleteRepository(context.Background(), me.Login, name) })
	var label api.Label
	if _, err = c.JSON(ctx, "POST", "repos/"+full+"/labels", map[string]string{"name": "bug", "color": "ff0000"}, &label); err != nil {
		t.Fatal(err)
	}
	var milestone api.Milestone
	if _, err = c.JSON(ctx, "POST", "repos/"+full+"/milestones", map[string]string{"title": "v1"}, &milestone); err != nil {
		t.Fatal(err)
	}
	r, env := envRunner(t, inst)
	invoke := func(args ...string) result {
		t.Helper()
		args = append(append([]string{"issue"}, args...), "-R", full)
		res := r.run(env, "", args...)
		if res.code != 0 {
			t.Fatalf("%v: exit %d stderr %s", args, res.code, res.stderr)
		}
		return res
	}
	created := invoke("create", "--title", "First", "--body", "Details", "--label", "bug", "--assignee", "@me", "--milestone", "v1")
	if !strings.Contains(created.stdout, "/issues/1") {
		t.Fatalf("create URL %s", created.stdout)
	}
	invoke("create", "--title", "Second", "--body", "Other")
	// Forgejo updates its issue search index asynchronously.
	deadline := time.Now().Add(15 * time.Second)
	for {
		found, err := c.ListIssues(ctx, me.Login, name, api.IssueSearch{Search: "First"}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("issue search index did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	listed := invoke("list", "--author", me.Login, "--assignee", me.Login, "--label", "bug", "--milestone", "v1", "--search", "First", "--json", "number,title", "--limit", "1")
	var items []struct {
		Number int
		Title  string
	}
	if err := json.Unmarshal([]byte(listed.stdout), &items); err != nil || len(items) != 1 || items[0].Number != 1 {
		t.Fatalf("filtered list %s (%v)", listed.stdout, err)
	}
	invoke("view", strings.TrimSpace(created.stdout), "--json", "number,title,body,labels,assignees,milestone")
	invoke("comment", "#1", "--body", "Discussion")
	viewed := invoke("view", "1", "--comments")
	if !strings.Contains(viewed.stdout, "Discussion") {
		t.Fatal(viewed.stdout)
	}
	invoke("close", "1", "--comment", "Closing")
	issue, err := c.GetIssue(ctx, me.Login, name, 1)
	if err != nil || issue.State != "closed" {
		t.Fatalf("close: %+v %v", issue, err)
	}
	invoke("reopen", "1", "--comment", "Reopening")
	invoke("edit", "1", "--title", "Changed", "--body", "", "--remove-label", "bug", "--remove-assignee", me.Login, "--remove-milestone")
	issue, err = c.GetIssue(ctx, me.Login, name, 1)
	if err != nil || issue.Title != "Changed" || issue.Body != "" || issue.State != "open" || len(issue.Labels) != 0 || len(issue.Assignees) != 0 || issue.Milestone != nil {
		t.Fatalf("edit: %+v %v", issue, err)
	}
	invoke("edit", "1", "--add-label", "bug", "--add-assignee", me.Login, "--milestone", strconv.FormatInt(milestone.ID, 10))
	invoke("pin", "1")
	invoke("unpin", "1")
	invoke("delete", "1", "--yes")
	if _, err := c.GetIssue(ctx, me.Login, name, 1); fjoerr.KindOf(err) != fjoerr.KindNotFound {
		t.Fatalf("deleted issue: %v", err)
	}
}

//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/fjoerr"
)

func TestReadReleasesAgainstForgejo(t *testing.T) {
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
	name := fmt.Sprintf("fjo-release-read-%d", time.Now().UnixNano())
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
	r, env := envRunner(t, inst)
	invoke := func(code fjoerr.ExitCode, want string, args ...string) {
		t.Helper()
		res := r.run(env, "", append(append([]string{"release"}, args...), "-R", full)...)
		if res.code != code || res.stdout != want {
			t.Fatalf("release %v: exit %d, stdout %q, stderr %q", args, res.code, res.stdout, res.stderr)
		}
	}
	invoke(fjoerr.ExitNotFound, "", "view")
	for _, opts := range []api.CreateReleaseOptions{
		{TagName: "releases/v1", Name: "Version One", Body: "Stable notes", TargetCommitish: repo.DefaultBranch},
		{TagName: "v2-rc", Name: "Candidate", Prerelease: true, TargetCommitish: repo.DefaultBranch},
		{TagName: "v3-draft", Name: "Draft", Draft: true, TargetCommitish: repo.DefaultBranch},
	} {
		if err := client.JSON(ctx, http.MethodPost, "repos/"+full+"/releases", opts, nil); err != nil {
			t.Fatalf("seed release %s: %v", opts.TagName, err)
		}
	}
	var direct api.Release
	if err := client.JSON(ctx, http.MethodGet, "repos/"+full+"/releases/tags/v3-draft", nil, &direct); err != nil || !direct.Draft {
		t.Fatalf("direct draft lookup: got %+v, error %v", direct, err)
	}
	invoke(0, "releases/v1\n", "view", "--json", "tagName", "--jq", ".tagName")
	invoke(0, "Stable notes\n", "view", "releases/v1", "--json", "body", "--jq", ".body")
	invoke(0, "true\n", "view", "v3-draft", "--json", "isDraft", "--jq", ".isDraft")
	invoke(0, "releases/v1\nv2-rc\nv3-draft\n", "list", "--json", "tagName", "--jq", "sort_by(.tagName) | .[].tagName")
	invoke(0, "releases/v1\n", "list", "--exclude-drafts", "--exclude-pre-releases", "--json", "tagName", "--jq", ".[].tagName")
	release, err := client.ReleaseByTag(ctx, me.Login, name, "releases/v1")
	if err != nil || !strings.Contains(release.HTMLURL, "/releases/tag/") {
		t.Fatalf("unexpected release browser URL: %+v %v", release, err)
	}
}

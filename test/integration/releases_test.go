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

func TestWriteReleasesAgainstForgejo(t *testing.T) {
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
	name := fmt.Sprintf("fjo-release-write-%d", time.Now().UnixNano())
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
	invoke := func(code fjoerr.ExitCode, input string, args ...string) result {
		t.Helper()
		res := r.run(env, input, append(append([]string{"release"}, args...), "-R", full)...)
		if res.code != code {
			t.Fatalf("release %v: exit %d, stdout %q, stderr %q", args, res.code, res.stdout, res.stderr)
		}
		return res
	}
	const defaultTag = "default-target"
	invoke(0, "", "create", defaultTag)
	defaultRelease, err := client.ReleaseByTag(ctx, me.Login, name, defaultTag)
	if err != nil || defaultRelease.TargetCommitish != repo.DefaultBranch || defaultRelease.Name != defaultTag {
		t.Fatalf("default release target and title: %+v, %v", defaultRelease, err)
	}
	if err := client.VerifyTag(ctx, me.Login, name, defaultTag); err != nil {
		t.Fatal(err)
	}
	invoke(fjoerr.ExitNotFound, "", "create", "absent", "--verify-tag")
	invoke(fjoerr.ExitUnsupported, "", "create", "assets-later", "bundle.bin")
	const draft = "preview/start"
	const published = "preview/published"
	invoke(0, "Original notes\n", "create", draft, "--title", "Preview", "--draft", "--prerelease",
		"--notes-file", "-", "--target", repo.DefaultBranch, "--hide-archive-links")
	invoke(fjoerr.ExitConflict, "", "create", draft)
	initial, err := client.ReleaseByTag(ctx, me.Login, name, draft)
	if err != nil || !initial.Draft || !initial.Prerelease || !initial.HideArchiveLinks || initial.Body != "Original notes\n" {
		t.Fatalf("unexpected draft: %+v, %v", initial, err)
	}
	invoke(fjoerr.ExitUnsupported, "", "edit", draft, "--notes=", "--draft=false")
	invoke(fjoerr.ExitUnsupported, "", "edit", draft, "--title=")
	const target = "release-target"
	if err := client.JSON(ctx, http.MethodPost, "repos/"+full+"/branches",
		map[string]string{"new_branch_name": target, "old_branch_name": repo.DefaultBranch}, nil); err != nil {
		t.Fatal(err)
	}
	invoke(0, "", "edit", draft, "--tag", published, "--title", "Final", "--notes", "Final notes", "--draft=false", "--prerelease=false", "--target", target)
	final, err := client.ReleaseByTag(ctx, me.Login, name, published)
	if err != nil || final.Draft || final.Prerelease || !final.HideArchiveLinks || final.Name != "Final" || final.Body != "Final notes" || final.ID != initial.ID || final.TargetCommitish != target {
		t.Fatalf("unexpected published state: %+v, %v", final, err)
	}
	if err := client.VerifyTag(ctx, me.Login, name, published); err != nil {
		t.Fatalf("publishing did not create the Git tag: %v", err)
	}
	const publishedTag = "preview/published-new"
	invoke(0, "", "edit", published, "--tag", publishedTag, "--target", repo.DefaultBranch)
	renamed, err := client.ReleaseByTag(ctx, me.Login, name, publishedTag)
	if err != nil || renamed.ID != final.ID || renamed.TargetCommitish != repo.DefaultBranch {
		t.Fatalf("published release rename: %+v, %v", renamed, err)
	}
	invoke(fjoerr.ExitUsage, "", "delete", publishedTag)
	invoke(0, "", "delete", publishedTag, "--yes")
	if err := client.VerifyTag(ctx, me.Login, name, publishedTag); err != nil {
		t.Fatalf("default deletion removed Git tag: %v", err)
	}
	invoke(0, "", "create", publishedTag, "--verify-tag", "--notes", "Restored release")
	invoke(0, "", "delete", publishedTag, "--yes", "--cleanup-tag")
	if err := client.VerifyTag(ctx, me.Login, name, publishedTag); fjoerr.KindOf(err) != fjoerr.KindNotFound {
		t.Fatalf("Git tag survived --cleanup-tag: %v", err)
	}
	if _, err := client.ReleaseByTag(ctx, me.Login, name, publishedTag); fjoerr.KindOf(err) != fjoerr.KindNotFound {
		t.Fatalf("release survived deletion: %v", err)
	}
}

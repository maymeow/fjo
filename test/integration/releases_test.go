//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	invoke(fjoerr.ExitUsage, "", "create", "assets-later", "bundle.bin")
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

func TestReleaseAssetsAgainstForgejo(t *testing.T) {
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
	name := fmt.Sprintf("fjo-release-assets-%d", time.Now().UnixNano())
	if _, err := client.CreateRepository(ctx, "", api.CreateRepoOptions{Name: name, AutoInit: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.DeleteRepository(context.Background(), me.Login, name); err != nil {
			t.Errorf("clean up test repository: %v", err)
		}
	})
	full := me.Login + "/" + name
	r, env := envRunner(t, inst)
	invoke := func(code fjoerr.ExitCode, args ...string) {
		t.Helper()
		res := r.run(env, "", append(append([]string{"release"}, args...), "-R", full)...)
		if res.code != code {
			t.Fatalf("release %v: exit %d, stdout %q, stderr %q", args, res.code, res.stdout, res.stderr)
		}
	}
	dir := t.TempDir()
	write := func(file string, content []byte) string {
		t.Helper()
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	notes := write("notes.txt", []byte("first"))
	invoke(0, "create", "v1", notes, "--title", "One")
	release, err := client.ReleaseByTag(ctx, me.Login, name, "v1")
	if err != nil || release.Draft || len(release.Assets) != 1 || release.Assets[0].Name != "notes.txt" {
		t.Fatalf("release created with an asset: %+v, %v", release, err)
	}
	large := make([]byte, 3<<20)
	for i := range large {
		large[i] = byte(i * 7)
	}
	bundle := write("bundle.bin", large)
	invoke(0, "upload", "v1", bundle)
	invoke(fjoerr.ExitConflict, "upload", "v1", notes)
	replaced := []byte("replaced content")
	write("notes.txt", replaced)
	invoke(0, "upload", "v1", notes, "--clobber")
	release, err = client.ReleaseByTag(ctx, me.Login, name, "v1")
	if err != nil {
		t.Fatal(err)
	}
	assets := map[string]api.Attachment{}
	for _, asset := range release.Assets {
		assets[asset.Name] = asset
	}
	if len(assets) != 2 || assets["notes.txt"].Size != int64(len(replaced)) || assets["bundle.bin"].Size != int64(len(large)) {
		t.Fatalf("unexpected assets after clobber: %+v", release.Assets)
	}
	for file, want := range map[string][]byte{"notes.txt": replaced, "bundle.bin": large} {
		if got := downloadAsset(t, client, assets[file].DownloadURL); !bytes.Equal(got, want) {
			t.Fatalf("%s: downloaded %d bytes that differ from the upload", file, len(got))
		}
	}
	invoke(fjoerr.ExitUsage, "delete-asset", "v1", "bundle.bin")
	invoke(0, "delete-asset", "v1", "bundle.bin", "--yes")
	invoke(fjoerr.ExitNotFound, "delete-asset", "v1", "bundle.bin", "--yes")
	release, err = client.ReleaseByTag(ctx, me.Login, name, "v1")
	if err != nil || len(release.Assets) != 1 || release.Assets[0].Name != "notes.txt" {
		t.Fatalf("asset survived deletion: %+v, %v", release, err)
	}
}

func downloadAsset(t *testing.T, client *api.Client, link string) []byte {
	t.Helper()
	req, err := client.NewRequest(t.Context(), http.MethodGet, link, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := api.CheckResponse(resp); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestReleaseDownloadAgainstForgejo(t *testing.T) {
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
	name := fmt.Sprintf("fjo-release-download-%d", time.Now().UnixNano())
	private := true
	if _, err := client.CreateRepository(ctx, "", api.CreateRepoOptions{Name: name, AutoInit: true, Private: &private}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.DeleteRepository(context.Background(), me.Login, name); err != nil {
			t.Errorf("clean up test repository: %v", err)
		}
	})
	full := me.Login + "/" + name
	r, env := envRunner(t, inst)
	invoke := func(code fjoerr.ExitCode, args ...string) result {
		t.Helper()
		res := r.run(env, "", append(append([]string{"release"}, args...), "-R", full)...)
		if res.code != code {
			t.Fatalf("release %v: exit %d, stdout %q, stderr %q", args, res.code, res.stdout, res.stderr)
		}
		return res
	}
	src := t.TempDir()
	payload := make([]byte, 3<<20+17)
	for i := range payload {
		payload[i] = byte(i * 13)
	}
	sum := sha256.Sum256(payload)
	files := map[string][]byte{
		"app.bin":       payload,
		"checksums.txt": []byte(hex.EncodeToString(sum[:]) + "  app.bin\n"),
		"bad.bin":       []byte("bad"),
		"SHA256SUMS":    []byte(strings.Repeat("00", 32) + "  bad.bin\n"),
	}
	for file, content := range files {
		if err := os.WriteFile(filepath.Join(src, file), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	invoke(0, "create", "v1", filepath.Join(src, "app.bin"), filepath.Join(src, "checksums.txt"), "--title", "One")
	dst := t.TempDir()
	invoke(0, "download", "-D", dst)
	for _, file := range []string{"app.bin", "checksums.txt"} {
		got, err := os.ReadFile(filepath.Join(dst, file))
		if err != nil || !bytes.Equal(got, files[file]) {
			t.Fatalf("%s differs after download (%d bytes): %v", file, len(got), err)
		}
	}
	invoke(fjoerr.ExitConflict, "download", "v1", "-D", dst, "-p", "*.bin")
	invoke(0, "download", "v1", "-D", dst, "-p", "*.bin", "--skip-existing")
	if res := invoke(0, "download", "v1", "-p", "checksums.txt", "-O", "-"); res.stdout != string(files["checksums.txt"]) {
		t.Fatalf("stdout download: %q", res.stdout)
	}
	archive := invoke(0, "download", "v1", "-A", "tar.gz", "-D", dst)
	info, err := os.Stat(filepath.Join(dst, name+"-v1.tar.gz"))
	if err != nil || info.Size() == 0 {
		t.Fatalf("archive download: %v, %+v", err, archive)
	}

	invoke(0, "create", "v2", filepath.Join(src, "bad.bin"), filepath.Join(src, "SHA256SUMS"), "--prerelease")
	bad := t.TempDir()
	invoke(fjoerr.ExitError, "download", "v2", "-D", bad, "-p", "bad.bin")
	if entries, err := os.ReadDir(bad); err != nil || len(entries) != 0 {
		t.Fatalf("checksum mismatch left files: %v %v", entries, err)
	}
	invoke(0, "download", "v2", "-D", bad, "-p", "bad.bin", "--skip-verify")
}

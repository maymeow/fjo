package app

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
	"github.com/maymeow/fjo/internal/reporef"
)

// gitRepo creates a repository with remotes ("name=url") and points the
// factory at it.
func gitRepo(t *testing.T, f *Factory, remotes ...string) *git.Client {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	f.GitDir = t.TempDir()
	c := f.Git()
	ctx := context.Background()
	if _, err := c.Run(ctx, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	for _, r := range remotes {
		name, u, _ := strings.Cut(r, "=")
		if _, err := c.Run(ctx, "remote", "add", name, u); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func knownHosts(ids ...string) *config.Config {
	cfg := config.New()
	for _, id := range ids {
		cfg.AddUser(id, "alice")
	}
	return cfg
}

func TestResolveRepoFlagAndEnv(t *testing.T) {
	f := factoryWithConfig(t, knownHosts("git.example.com"), map[string]string{"FJO_REPO": "env/repo"})
	f.GitDir = t.TempDir() // not a repository: flag and env must not need git

	ref, err := f.ResolveRepo(context.Background(), "flag/repo")
	if err != nil || ref != (reporef.Ref{Host: "git.example.com", Owner: "flag", Name: "repo"}) {
		t.Errorf("flag: %+v, %v", ref, err)
	}
	ref, err = f.ResolveRepo(context.Background(), "")
	if err != nil || ref.FullName() != "env/repo" {
		t.Errorf("FJO_REPO: %+v, %v", ref, err)
	}
}

func TestResolveRepoInvalidEnv(t *testing.T) {
	f := factoryWithConfig(t, knownHosts("git.example.com"), map[string]string{"FJO_REPO": "not-a-repo"})
	_, err := f.ResolveRepo(context.Background(), "")
	if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(err.Error(), "FJO_REPO") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveRepoSingleRemote(t *testing.T) {
	f := factoryWithConfig(t, knownHosts("git.example.com"), nil)
	gitRepo(t, f,
		"origin=git@git.example.com:me/project.git",
		"mirror=https://git.example.com/me/project.git", // same repository
		"github=https://github.com/me/project.git",      // unknown host: ignored
	)
	ref, err := f.ResolveRepo(context.Background(), "")
	if err != nil || ref != (reporef.Ref{Host: "git.example.com", Owner: "me", Name: "project"}) {
		t.Errorf("ResolveRepo = %+v, %v", ref, err)
	}
}

func TestResolveRepoDoesNotPreferOrigin(t *testing.T) {
	f := factoryWithConfig(t, knownHosts("git.example.com"), nil)
	gitRepo(t, f,
		"origin=https://git.example.com/me/project.git",
		"upstream=https://git.example.com/org/project.git",
	)
	_, err := f.ResolveRepo(context.Background(), "")
	if fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Fatalf("err = %v, want a usage error", err)
	}
	for _, want := range []string{"me/project (origin)", "org/project (upstream)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if !strings.Contains(fjoerr.HintOf(err), "set-default") {
		t.Errorf("hint = %q", fjoerr.HintOf(err))
	}
}

func TestResolveRepoDefault(t *testing.T) {
	f := factoryWithConfig(t, knownHosts("git.example.com"), nil)
	c := gitRepo(t, f,
		"origin=https://git.example.com/me/project.git",
		"upstream=https://git.example.com/org/project.git",
	)
	ctx := context.Background()
	if err := c.SetResolved(ctx, "upstream", "base"); err != nil {
		t.Fatal(err)
	}
	ref, err := f.ResolveRepo(ctx, "")
	if err != nil || ref.FullName() != "org/project" {
		t.Errorf("base: %+v, %v", ref, err)
	}

	if err := c.UnsetResolved(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResolved(ctx, "origin", "git.example.com/other/repo"); err != nil {
		t.Fatal(err)
	}
	ref, err = f.ResolveRepo(ctx, "")
	if err != nil || ref.FullName() != "other/repo" {
		t.Errorf("explicit value: %+v, %v", ref, err)
	}

	// The flag still wins over the stored default.
	ref, err = f.ResolveRepo(ctx, "flag/repo")
	if err != nil || ref.FullName() != "flag/repo" {
		t.Errorf("flag over default: %+v, %v", ref, err)
	}
}

func TestResolveRepoBrokenDefault(t *testing.T) {
	f := factoryWithConfig(t, knownHosts("git.example.com"), nil)
	c := gitRepo(t, f, "origin=https://git.example.com/me/project.git")
	if err := c.SetResolved(context.Background(), "origin", "garbage"); err != nil {
		t.Fatal(err)
	}
	_, err := f.ResolveRepo(context.Background(), "")
	if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(fjoerr.HintOf(err), "set-default") {
		t.Errorf("err = %v, hint %q", err, fjoerr.HintOf(err))
	}
}

func TestResolveRepoNoKnownRemote(t *testing.T) {
	f := factoryWithConfig(t, knownHosts("git.example.com"), nil)
	gitRepo(t, f, "origin=https://github.com/me/project.git")
	_, err := f.ResolveRepo(context.Background(), "")
	if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(err.Error(), "known Forgejo host") {
		t.Errorf("err = %v", err)
	}

	f = factoryWithConfig(t, knownHosts("git.example.com"), nil)
	gitRepo(t, f)
	_, err = f.ResolveRepo(context.Background(), "")
	if !strings.Contains(fjoerr.HintOf(err), "no remotes") {
		t.Errorf("no remotes: err %v, hint %q", err, fjoerr.HintOf(err))
	}
}

func TestResolveRepoWithoutConfiguredHosts(t *testing.T) {
	// With FJO_HOST, only remotes on that host count.
	f := factoryWithConfig(t, config.New(), map[string]string{"FJO_HOST": "codeberg.org"})
	gitRepo(t, f,
		"origin=git@codeberg.org:me/project.git",
		"gh=https://github.com/me/project.git",
	)
	ref, err := f.ResolveRepo(context.Background(), "")
	if err != nil || ref.Host != "codeberg.org" {
		t.Errorf("FJO_HOST: %+v, %v", ref, err)
	}

	// Without any known host no remote is guessed to be Forgejo.
	f = factoryWithConfig(t, config.New(), nil)
	gitRepo(t, f, "origin=git@codeberg.org:me/project.git", "local=/srv/git/project.git")
	_, err = f.ResolveRepo(context.Background(), "")
	if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(fjoerr.HintOf(err), "auth login") {
		t.Errorf("no hosts: err %v, hint %q", err, fjoerr.HintOf(err))
	}
}

func TestResolveRepoNotARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	f := factoryWithConfig(t, knownHosts("git.example.com"), nil)
	f.GitDir = t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", f.GitDir)
	_, err := f.ResolveRepo(context.Background(), "")
	if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(fjoerr.HintOf(err), "--repo") {
		t.Errorf("err = %v, hint %q", err, fjoerr.HintOf(err))
	}
}

func TestParseRepoNeedsHost(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	if _, err := f.ParseRepo("owner/repo"); fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Errorf("err = %v, want usage (no host)", err)
	}
	if _, err := f.ParseRepo("bad"); fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Errorf("err = %v, want usage", err)
	}
}

package pr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
	"github.com/maymeow/fjo/internal/iostreams"
	"github.com/maymeow/fjo/internal/reporef"
)

func gitFixture(t *testing.T, directory string, args ...string) string {
	t.Helper()
	g := &git.Client{Dir: directory}
	out, err := g.Run(t.Context(), args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(out)
}

func commitFixture(t *testing.T, directory, title string) {
	t.Helper()
	gitFixture(t, directory, "-c", "user.name=Test", "-c", "user.email=test@example.org",
		"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", title)
}

func gitFactory(t *testing.T, directory string) *app.Factory {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	streams, _, _, _ := iostreams.Test()
	configDir := t.TempDir()
	f := app.New(streams, func(key string) string {
		if key == "FJO_CONFIG_DIR" {
			return configDir
		}
		return ""
	}, build.Info{Version: "test"})
	f.GitDir = directory
	return f
}

func TestCheckoutFromBareRepository(t *testing.T) {
	work := t.TempDir()
	f := gitFactory(t, work)
	bare := t.TempDir()
	gitFixture(t, bare, "init", "--bare")
	gitFixture(t, work, "init", "--initial-branch=main")
	commitFixture(t, work, "Base")
	base := gitFixture(t, work, "rev-parse", "HEAD")
	gitFixture(t, work, "switch", "-c", "upstream-source")
	commitFixture(t, work, "Pull request")
	head := gitFixture(t, work, "rev-parse", "HEAD")
	const pullRef = "refs/pull/42/head"
	gitFixture(t, work, "push", "--", bare, "HEAD:"+pullRef)
	gitFixture(t, work, "switch", "main")
	runCheckout := func(force bool) error {
		return checkoutFrom(t.Context(), f, bare, pullRef, "topic", head, force)
	}
	dirty := filepath.Join(work, "untracked")
	if err := os.WriteFile(dirty, []byte("local data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runCheckout(true); fjoerr.KindOf(err) != fjoerr.KindConflict {
		t.Fatalf("dirty checkout: %v", err)
	}
	if err := os.Remove(dirty); err != nil {
		t.Fatal(err)
	}
	if err := runCheckout(false); err != nil {
		t.Fatal(err)
	}
	if tip := gitFixture(t, work, "rev-parse", "HEAD"); tip != head {
		t.Fatalf("checkout tip = %s, want %s", tip, head)
	}
	gitFixture(t, work, "switch", "main")
	gitFixture(t, work, "branch", "-f", "topic", base)
	if err := runCheckout(false); err != nil {
		t.Fatalf("fast-forward checkout: %v", err)
	}
	commitFixture(t, work, "Local commit")
	local := gitFixture(t, work, "rev-parse", "HEAD")
	if err := runCheckout(false); fjoerr.KindOf(err) != fjoerr.KindConflict {
		t.Fatalf("diverged checkout: %v", err)
	}
	if tip := gitFixture(t, work, "rev-parse", "HEAD"); tip != local {
		t.Fatalf("moved local branch to %s", tip)
	}
	if err := runCheckout(true); err != nil {
		t.Fatalf("forced checkout: %v", err)
	}
	if tip := gitFixture(t, work, "rev-parse", "HEAD"); tip != head {
		t.Fatalf("forced checkout tip = %s, want %s", tip, head)
	}
}

func TestCreateChoosesConfiguredForkAndTrackingBranch(t *testing.T) {
	dir := t.TempDir()
	f := gitFactory(t, dir)
	gitFixture(t, dir, "init", "--initial-branch=feature")
	commitFixture(t, dir, "Initial")
	gitFixture(t, dir, "remote", "add", "upstream", "https://forge.example/org/repo.git")
	gitFixture(t, dir, "remote", "add", "fork", "https://forge.example/alice/repo.git")
	gitFixture(t, dir, "config", "branch.feature.remote", "fork")
	gitFixture(t, dir, "config", "branch.feature.merge", "refs/heads/remote-topic")
	base := reporef.Ref{Host: "forge.example", Owner: "org", Name: "repo"}
	head, err := findCreateHead(t.Context(), f, base, "")
	if err != nil || head.remote != "fork" || head.repo.Owner != "alice" || head.branch != "remote-topic" {
		t.Fatalf("head = %+v, err = %v", head, err)
	}
	gitFixture(t, dir, "config", "branch.feature.pushRemote", "upstream")
	head, err = findCreateHead(t.Context(), f, base, "")
	if err != nil || !head.repo.Equal(base) || head.remote != "upstream" || head.branch != "feature" {
		t.Fatalf("configured push head = %+v, err = %v", head, err)
	}
	gitFixture(t, dir, "checkout", "--detach")
	if _, err := findCreateHead(t.Context(), f, base, ""); fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Fatalf("detached HEAD error = %v", err)
	}
}

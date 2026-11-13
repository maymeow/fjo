package git

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		in   string
		want URL
	}{
		{"https://git.example.com/owner/repo.git", URL{"https", "git.example.com", "", "owner/repo.git"}},
		{"https://user:secret@Git.Example.com:8443/forgejo/owner/repo/", URL{"https", "git.example.com", "8443", "forgejo/owner/repo"}},
		{"http://localhost:3000/o/r", URL{"http", "localhost", "3000", "o/r"}},
		{"ssh://git@git.example.com/owner/repo.git", URL{"ssh", "git.example.com", "", "owner/repo.git"}},
		{"ssh://git@git.example.com:2222/owner/repo.git", URL{"ssh", "git.example.com", "2222", "owner/repo.git"}},
		{"git+ssh://git@host/o/r", URL{"ssh", "host", "", "o/r"}},
		{"git@git.example.com:owner/repo.git", URL{"ssh", "git.example.com", "", "owner/repo.git"}},
		{"forgejo-alias:owner/repo", URL{"ssh", "forgejo-alias", "", "owner/repo"}},
		{"git@[::1]:owner/repo.git", URL{"ssh", "::1", "", "owner/repo.git"}},
		{"git://git.example.com/owner/repo.git", URL{"git", "git.example.com", "", "owner/repo.git"}},
	}
	for _, tt := range tests {
		got, err := ParseURL(tt.in)
		if err != nil {
			t.Errorf("ParseURL(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseURL(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestParseURLRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"/srv/git/repo.git",
		"../other",
		"file:///srv/git/repo.git",
		`C:\repos\project`,
		"C:/repos/project",
		"ftp://example.com/o/r",
		"https:///o/r",
	} {
		if u, err := ParseURL(in); err == nil {
			t.Errorf("ParseURL(%q) = %+v, want an error", in, u)
		}
	}
}

// newRepo initializes a git repository with the given remotes in a temp dir.
func newRepo(t *testing.T, remotes ...[2]string) *Client {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	c := &Client{Dir: t.TempDir()}
	ctx := context.Background()
	mustRun(t, c, "init", "-q", "-b", "main")
	for _, r := range remotes {
		mustRun(t, c, "remote", "add", r[0], r[1])
	}
	_ = ctx
	return c
}

func mustRun(t *testing.T, c *Client, args ...string) string {
	t.Helper()
	out, err := c.Run(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func TestRemotesAndResolved(t *testing.T) {
	c := newRepo(t,
		[2]string{"upstream", "https://git.example.com/org/project.git"},
		[2]string{"origin", "git@git.example.com:me/project.git"},
	)
	mustRun(t, c, "remote", "set-url", "--push", "origin", "git@git.example.com:me/push.git")
	ctx := context.Background()

	remotes, err := c.Remotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 2 || remotes[0].Name != "origin" || remotes[1].Name != "upstream" {
		t.Fatalf("remotes = %+v", remotes)
	}
	if remotes[0].FetchURL != "git@git.example.com:me/project.git" || remotes[0].PushURL != "git@git.example.com:me/push.git" {
		t.Errorf("origin = %+v", remotes[0])
	}
	if remotes[0].Resolved != "" || remotes[1].Resolved != "" {
		t.Errorf("unexpected resolved marker: %+v", remotes)
	}

	if err := c.SetResolved(ctx, "upstream", "base"); err != nil {
		t.Fatal(err)
	}
	remotes, _ = c.Remotes(ctx)
	if remotes[1].Resolved != "base" {
		t.Errorf("upstream resolved = %q", remotes[1].Resolved)
	}

	if err := c.UnsetResolved(ctx); err != nil {
		t.Fatal(err)
	}
	remotes, _ = c.Remotes(ctx)
	if remotes[1].Resolved != "" {
		t.Errorf("resolved not removed: %+v", remotes[1])
	}
	// Unsetting with nothing set is not an error.
	if err := c.UnsetResolved(ctx); err != nil {
		t.Errorf("second UnsetResolved: %v", err)
	}
}

func TestNoRemotes(t *testing.T) {
	c := newRepo(t)
	remotes, err := c.Remotes(context.Background())
	if err != nil || len(remotes) != 0 {
		t.Errorf("Remotes = %+v, %v", remotes, err)
	}
}

func TestCurrentBranch(t *testing.T) {
	c := newRepo(t)
	ctx := context.Background()
	if b, err := c.CurrentBranch(ctx); err != nil || b != "main" {
		t.Errorf("CurrentBranch = %q, %v", b, err)
	}
	mustRun(t, c, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")
	mustRun(t, c, "checkout", "-q", "--detach")
	if _, err := c.CurrentBranch(ctx); err == nil {
		t.Error("CurrentBranch on a detached HEAD should fail")
	}
}

func TestTopLevel(t *testing.T) {
	c := newRepo(t)
	if top, err := c.TopLevel(context.Background()); err != nil || top == "" {
		t.Errorf("TopLevel = %q, %v", top, err)
	}
}

func TestNotRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	c := &Client{Dir: t.TempDir()}
	t.Setenv("GIT_CEILING_DIRECTORIES", c.Dir)
	if _, err := c.Remotes(context.Background()); !errors.Is(err, ErrNotRepository) {
		t.Errorf("err = %v, want ErrNotRepository", err)
	}
}

func TestGitMissing(t *testing.T) {
	c := &Client{Dir: t.TempDir(), Path: "/nonexistent/git"}
	if _, err := c.Run(context.Background(), "status"); err == nil {
		t.Error("running a missing git binary should fail")
	}
}

func TestErrorMessage(t *testing.T) {
	c := newRepo(t)
	_, err := c.Run(context.Background(), "rev-parse", "--verify", "does-not-exist")
	var ge *Error
	if !errors.As(err, &ge) || ge.ExitCode == 0 || ge.Error() == "" {
		t.Errorf("err = %#v", err)
	}
}

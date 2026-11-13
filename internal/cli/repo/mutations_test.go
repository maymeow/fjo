package repo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
)

// bareRepo creates a bare repository and returns an https clone URL that git
// rewrites to it (url.<path>.insteadOf), because fjo only accepts network
// clone URLs from the server.
func bareRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	c := &git.Client{Dir: dir}
	if _, err := c.Run(context.Background(), "init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	u := "https://forge.invalid/" + filepath.Base(dir) + ".git"
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+dir+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", u)
	return u
}

func repoResponseJSON(fullName, name, cloneURL string) string {
	b, _ := json.Marshal(map[string]any{
		"full_name": fullName,
		"name":      name,
		"html_url":  "https://forge/" + fullName,
		"clone_url": cloneURL,
		"ssh_url":   "git@forge:" + fullName + ".git",
	})
	return string(b)
}

func mutationHandler(cloneURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			if strings.Contains(readBody(r), `"name":"taken"`) {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"message":"repository already exists"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, repoResponseJSON("alice/demo", "demo", cloneURL))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/org/repos":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, repoResponseJSON("org/demo", "demo", cloneURL))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/org/project":
			_, _ = io.WriteString(w, repoResponseJSON("org/project", "project", cloneURL))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/up/project":
			_, _ = io.WriteString(w, repoResponseJSON("up/project", "project", cloneURL))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/up/project/forks":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, repoResponseJSON("alice/project", "project", cloneURL))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/sync_fork"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/repos/"):
			_, _ = io.WriteString(w, repoResponseJSON("org/project", "project", cloneURL))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/repos/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"not found"}`)
		}
	}
}

func readBody(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(b)))
	return string(b)
}

func TestCreateRepositoryCommand(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	r := h.run(nil, false, "", "repo", "create", "demo", "--private",
		"--description", "Example", "--initialize", "--gitignore", "Go",
		"--license", "MIT", "--default-branch", "main")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if r.stdout != "https://forge/alice/demo\n" {
		t.Errorf("stdout = %q", r.stdout)
	}
	if h.methods[0] != http.MethodPost || h.reqs[0].Path != "/api/v1/user/repos" {
		t.Errorf("request = %s %s", h.methods[0], h.reqs[0].Path)
	}
	for _, want := range []string{
		`"name":"demo"`, `"description":"Example"`, `"private":true`,
		`"auto_init":true`, `"gitignores":"Go"`, `"license":"MIT"`, `"default_branch":"main"`,
	} {
		if !strings.Contains(h.bodies[0], want) {
			t.Errorf("body %s missing %s", h.bodies[0], want)
		}
	}
}

func TestCreateRepositoryOwnerForms(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	if r := h.run(nil, false, "", "repo", "create", "demo", "--owner", "org", "--public"); r.code != fjoerr.ExitOK {
		t.Fatalf("owner flag: exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.reqs[0].Path; got != "/api/v1/orgs/org/repos" {
		t.Errorf("path = %q", got)
	}
	if strings.Contains(h.bodies[0], `"private":true`) {
		t.Error("--public must not send private=true")
	}
	if !strings.Contains(h.bodies[0], `"private":false`) {
		t.Error("--public should send private=false to override a server default")
	}

	if r := h.run(nil, false, "", "repo", "create", "org/demo"); r.code != fjoerr.ExitOK {
		t.Fatalf("OWNER/NAME: exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.reqs[1].Path; got != "/api/v1/orgs/org/repos" {
		t.Errorf("path = %q", got)
	}
}

func TestCreateRepositoryErrors(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	for _, args := range [][]string{
		{"repo", "create"},
		{"repo", "create", "demo", "--private", "--public"},
		{"repo", "create", "demo", "--gitignore", "Go"},
		{"repo", "create", "org/demo", "--owner", "org"},
		{"repo", "create", "org/demo/extra"},
		{"repo", "create", "demo", "extra"},
	} {
		r := h.run(nil, false, "", args...)
		if r.code != fjoerr.ExitUsage {
			t.Errorf("%v: exit %d, stderr %q", args, r.code, r.stderr)
		}
	}
	if len(h.reqs) != 0 {
		t.Errorf("usage errors sent requests: %v", h.reqs)
	}

	r := h.run(nil, false, "", "repo", "create", "taken")
	if r.code != fjoerr.ExitConflict || !strings.Contains(r.stderr, "repository taken already exists") {
		t.Errorf("conflict: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestCreateRepositoryClone(t *testing.T) {
	bare := bareRepo(t)
	h := newHarness(t, mutationHandler(bare))
	r := h.run(nil, false, "", "repo", "create", "demo", "--clone")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(h.gitDir, "demo", ".git")); err != nil {
		t.Errorf("clone directory missing: %v", err)
	}
}

func TestCloneRepositoryCommand(t *testing.T) {
	bare := bareRepo(t)
	h := newHarness(t, mutationHandler(bare))

	r := h.run(nil, false, "", "repo", "clone", "org/project")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(h.gitDir, "project", ".git")); err != nil {
		t.Errorf("default destination missing: %v", err)
	}

	r = h.run(nil, false, "", "repo", "clone", "org/project", "custom")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(h.gitDir, "custom", ".git")); err != nil {
		t.Errorf("custom destination missing: %v", err)
	}

	r = h.run(nil, false, "", "repo", "clone", "org/missing")
	if r.code != fjoerr.ExitNotFound || !strings.Contains(r.stderr, "repository "+h.host()+"/org/missing not found") {
		t.Errorf("missing: exit %d, stderr %q", r.code, r.stderr)
	}
	if r = h.run(nil, false, "", "repo", "clone"); r.code != fjoerr.ExitUsage {
		t.Errorf("no args: exit %d", r.code)
	}
}

func TestForkRepositoryCommand(t *testing.T) {
	bare := bareRepo(t)
	h := newHarness(t, mutationHandler(bare))

	r := h.run(nil, false, "", "repo", "fork", "up/project", "--org", "my-team", "--name", "project-fork")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if r.stdout != "https://forge/alice/project\n" {
		t.Errorf("stdout = %q", r.stdout)
	}
	if got := h.reqs[1].Path; got != "/api/v1/repos/up/project/forks" {
		t.Errorf("fork path = %q", got)
	}
	for _, want := range []string{`"organization":"my-team"`, `"name":"project-fork"`} {
		if !strings.Contains(h.bodies[1], want) {
			t.Errorf("body %s missing %s", h.bodies[1], want)
		}
	}
}

func TestForkRepositoryCloneAddsUpstream(t *testing.T) {
	bare := bareRepo(t)
	h := newHarness(t, mutationHandler(bare))
	r := h.run(nil, false, "", "repo", "fork", "up/project", "--clone")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	dir := filepath.Join(h.gitDir, "project")
	remotes, err := (&git.Client{Dir: dir}).Remotes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rem := range remotes {
		if rem.Name == "upstream" && (rem.FetchURL == bare || strings.HasSuffix(rem.FetchURL, strings.TrimSuffix(filepath.Base(bare), ".git"))) {
			found = true
		}
	}
	if !found {
		t.Errorf("upstream remote not added: %+v", remotes)
	}
}

func TestForkConflict(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, repoResponseJSON("up/project", "project", ""))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"message":"repository already exists"}`)
	})
	r := h.run(nil, false, "", "repo", "fork", "up/project")
	if r.code != fjoerr.ExitConflict || !strings.Contains(r.stderr, "already exists") {
		t.Errorf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestEditRepositoryCommand(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	r := h.run(nil, false, "", "repo", "edit", "org/project",
		"--description", "new", "--homepage", "https://example.com",
		"--visibility", "private", "--default-branch", "develop")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if h.methods[0] != http.MethodPatch || h.reqs[0].Path != "/api/v1/repos/org/project" {
		t.Errorf("request = %s %s", h.methods[0], h.reqs[0].Path)
	}
	for _, want := range []string{
		`"description":"new"`, `"website":"https://example.com"`,
		`"private":true`, `"default_branch":"develop"`,
	} {
		if !strings.Contains(h.bodies[0], want) {
			t.Errorf("body %s missing %s", h.bodies[0], want)
		}
	}

	r = h.run(nil, false, "", "repo", "edit", "-R", "org/project", "--visibility", "public")
	if r.code != fjoerr.ExitOK || !strings.Contains(h.bodies[1], `"private":false`) {
		t.Errorf("public edit: exit %d, body %s, stderr %q", r.code, h.bodies[1], r.stderr)
	}
}

func TestEditRepositoryUsageErrors(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	for _, args := range [][]string{
		{"repo", "edit"},
		{"repo", "edit", "org/project"},
		{"repo", "edit", "org/project", "--visibility", "internal"},
		{"repo", "edit", "org/project", "--default-branch", ""},
		{"repo", "edit", "-R", "org/project", "org/other", "--description", "x"},
	} {
		r := h.run(nil, false, "", args...)
		if r.code != fjoerr.ExitUsage {
			t.Errorf("%v: exit %d, stderr %q", args, r.code, r.stderr)
		}
	}
	if len(h.reqs) != 0 {
		t.Errorf("usage errors sent requests: %v", h.reqs)
	}
}

func TestRenameRepositoryCommand(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	r := h.run(nil, false, "", "repo", "rename", "org/project", "new-name", "--yes")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if h.methods[0] != http.MethodPatch || h.reqs[0].Path != "/api/v1/repos/org/project" {
		t.Errorf("request = %s %s", h.methods[0], h.reqs[0].Path)
	}
	if !strings.Contains(h.bodies[0], `"name":"new-name"`) {
		t.Errorf("body = %s", h.bodies[0])
	}

	r = h.run(nil, false, "", "repo", "rename", "-R", "org/project", "other-name", "--yes")
	if r.code != fjoerr.ExitOK || !strings.Contains(h.bodies[1], `"name":"other-name"`) {
		t.Errorf("repo flag: exit %d, body %s, stderr %q", r.code, h.bodies[1], r.stderr)
	}
}

func TestRenameNeedsConfirmation(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	r := h.run(nil, false, "", "repo", "rename", "org/project", "new-name")
	if r.code != fjoerr.ExitUsage || !strings.Contains(r.stderr, "--yes") {
		t.Errorf("non-interactive: exit %d, stderr %q", r.code, r.stderr)
	}

	r = h.run(nil, true, "n\n", "repo", "rename", "org/project", "new-name")
	if r.code != fjoerr.ExitCancel {
		t.Errorf("declined: exit %d, stderr %q", r.code, r.stderr)
	}

	r = h.run(nil, true, "y\n", "repo", "rename", "org/project", "new-name")
	if r.code != fjoerr.ExitOK {
		t.Errorf("accepted: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestDeleteRepositoryCommand(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	r := h.run(nil, false, "", "repo", "delete", "org/project", "--yes")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if h.methods[0] != http.MethodDelete || h.reqs[0].Path != "/api/v1/repos/org/project" {
		t.Errorf("request = %s %s", h.methods[0], h.reqs[0].Path)
	}

	r = h.run(nil, false, "", "repo", "delete", "org/project")
	if r.code != fjoerr.ExitUsage || len(h.reqs) != 1 {
		t.Errorf("without --yes: exit %d, requests %d, stderr %q", r.code, len(h.reqs), r.stderr)
	}
}

func TestDeleteRequiresTypedName(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	for _, answer := range []string{"y\n", "yes\n", "org/other\n", "\n"} {
		r := h.run(nil, true, answer, "repo", "delete", "org/project")
		if r.code != fjoerr.ExitCancel || !strings.Contains(r.stderr, "nothing was deleted") {
			t.Errorf("answer %q: exit %d, stderr %q", answer, r.code, r.stderr)
		}
	}
	if len(h.reqs) != 0 {
		t.Fatalf("a wrong confirmation sent requests: %v", h.reqs)
	}

	r := h.run(nil, true, "org/project\n", "repo", "delete", "org/project")
	if r.code != fjoerr.ExitOK || len(h.reqs) != 1 {
		t.Fatalf("typed name: exit %d, requests %d, stderr %q", r.code, len(h.reqs), r.stderr)
	}
	if !strings.Contains(r.stderr, "Type org/project to confirm deletion") || !strings.Contains(r.stderr, "permanently deletes") {
		t.Errorf("prompt = %q", r.stderr)
	}

	r = h.run(nil, true, "  ORG/Project \n", "repo", "delete", "org/project")
	if r.code != fjoerr.ExitOK {
		t.Errorf("case-insensitive name: exit %d, stderr %q", r.code, r.stderr)
	}
	r = h.run(nil, true, h.host()+"/org/project\n", "repo", "delete", "org/project")
	if r.code != fjoerr.ExitOK {
		t.Errorf("HOST/OWNER/REPO: exit %d, stderr %q", r.code, r.stderr)
	}
}

// TestCloneRejectsMaliciousServer: clone URLs and names come from the server.
// A value git would read as an option must never reach git (it could run a
// local command through core.sshCommand or --upload-pack).
func TestCloneRejectsMaliciousServer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	mark := filepath.Join(t.TempDir(), "PWNED")
	tests := []struct{ name, cloneURL, repoName string }{
		{"option as URL", "--config=core.sshCommand=touch " + mark + " #", "evil:repo"},
		{"upload-pack", "--upload-pack=touch " + mark, "x"},
		{"ext transport", "ext::sh -c touch% " + mark, "x"},
		{"file URL", "file:///etc", "x"},
		{"local path", "/etc", "x"},
		{"whitespace", "https://forge.invalid/a b.git", "x"},
		{"name is an option", "https://forge.invalid/o/r.git", "--template=/tmp"},
		{"name with separator", "https://forge.invalid/o/r.git", "../escape"},
		{"name with colon", "https://forge.invalid/o/r.git", "evil:repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := json.Marshal(map[string]any{"full_name": "org/project", "name": tt.repoName, "clone_url": tt.cloneURL})
				_, _ = w.Write(b)
			})
			r := h.run(nil, false, "", "repo", "clone", "org/project")
			if r.code == fjoerr.ExitOK {
				t.Errorf("clone succeeded, stderr %q", r.stderr)
			}
			if _, err := os.Stat(mark); err == nil {
				t.Fatal("a command from the server response was executed")
			}
			if !strings.Contains(r.stderr, "refusing clone URL") && !strings.Contains(r.stderr, "as a directory") {
				t.Errorf("stderr = %q", r.stderr)
			}
		})
	}
}

// An explicit destination is used as-is even when the server name is odd.
func TestCloneExplicitDestinationWithOddServerName(t *testing.T) {
	u := bareRepo(t)
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, repoResponseJSON("org/project", "../odd", u))
	})
	r := h.run(nil, false, "", "repo", "clone", "org/project", "safe-dir")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(h.gitDir, "safe-dir", ".git")); err != nil {
		t.Errorf("clone missing: %v", err)
	}
}

func TestArchiveCommands(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	r := h.run(nil, false, "", "repo", "archive", "org/project", "--yes")
	if r.code != fjoerr.ExitOK || !strings.Contains(h.bodies[0], `"archived":true`) {
		t.Fatalf("archive: exit %d, body %s, stderr %q", r.code, h.bodies[0], r.stderr)
	}
	r = h.run(nil, false, "", "repo", "unarchive", "org/project")
	if r.code != fjoerr.ExitOK || !strings.Contains(h.bodies[1], `"archived":false`) {
		t.Fatalf("unarchive: exit %d, body %s, stderr %q", r.code, h.bodies[1], r.stderr)
	}
}

func TestSyncCommand(t *testing.T) {
	h := newHarness(t, mutationHandler(""))
	r := h.run(nil, false, "", "repo", "sync", "org/project")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.reqs[0].Path; got != "/api/v1/repos/org/project/sync_fork" {
		t.Errorf("path = %q", got)
	}

	r = h.run(nil, false, "", "repo", "sync", "org/project", "--branch", "feature/x")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("branch: exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.reqs[1].EscapedPath(); got != "/api/v1/repos/org/project/sync_fork/feature%2Fx" {
		t.Errorf("escaped path = %q", got)
	}
}

func TestViewRepoFlag(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, false, "", "repo", "view", "-R", "org/project")
	if r.code != fjoerr.ExitOK || !strings.Contains(r.stdout, "name:\torg/project") {
		t.Errorf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

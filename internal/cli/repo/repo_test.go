package repo_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
	"github.com/maymeow/fjo/internal/iostreams"
)

const repoJSON = `{"id":1,"name":"project","full_name":"org/project","owner":{"login":"org"},
"description":"A test project","private":true,"fork":true,"parent":{"full_name":"up/project"},
"archived":false,"default_branch":"main","stars_count":3,"forks_count":1,"open_issues_count":2,
"open_pr_counter":4,"html_url":"https://forge/org/project","clone_url":"https://forge/org/project.git",
"ssh_url":"git@forge:org/project.git","updated_at":"2026-11-01T10:00:00Z","topics":["cli","go"]}`

type harness struct {
	t         *testing.T
	srv       *httptest.Server
	reqs      []*url.URL
	gitDir    string
	opened    string
	noBrowser bool
}

func newHarness(t *testing.T, handler http.HandlerFunc) *harness {
	t.Helper()
	h := &harness{t: t, gitDir: t.TempDir()}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := *r.URL
		h.reqs = append(h.reqs, &u)
		if r.Header.Get("Authorization") != "token secret-token-value" {
			t.Errorf("%s without token", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) host() string { return strings.TrimPrefix(h.srv.URL, "https://") }

type result struct {
	code           fjoerr.ExitCode
	stdout, stderr string
}

func (h *harness) run(env map[string]string, tty bool, stdin string, args ...string) result {
	h.t.Helper()
	e := map[string]string{
		"FJO_CONFIG_DIR": h.t.TempDir(),
		"FJO_HOST":       h.host(),
		"FJO_TOKEN":      "secret-token-value",
	}
	for k, v := range env {
		e[k] = v
	}
	ios, in, out, errOut := iostreams.Test()
	in.WriteString(stdin)
	ios.SetTTY(tty, tty, tty)
	getenv := func(k string) string { return e[k] }
	ios.SetGetenv(getenv)
	f := app.New(ios, getenv, build.Info{Version: "1.0.0", Platform: "test/test"})
	f.HTTPTransport = h.srv.Client().Transport
	f.GitDir = h.gitDir
	f.Browser = func(u string) error {
		if h.noBrowser {
			return browser.ErrNoBrowser
		}
		h.opened = u
		return nil
	}
	code := cli.Run(context.Background(), f, args)
	return result{code, out.String(), errOut.String()}
}

// initGit turns the harness directory into a repository with remotes.
func (h *harness) initGit(remotes ...string) *git.Client {
	h.t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		h.t.Skip("git not installed")
	}
	c := &git.Client{Dir: h.gitDir}
	ctx := context.Background()
	if _, err := c.Run(ctx, "init", "-q"); err != nil {
		h.t.Fatal(err)
	}
	for _, r := range remotes {
		name, u, _ := strings.Cut(r, "=")
		hostname, _, _ := strings.Cut(h.host(), ":")
		u = strings.ReplaceAll(u, "HOSTNAME", hostname) // scp-like remotes have no port
		u = strings.ReplaceAll(u, "HOST", h.host())
		if _, err := c.Run(ctx, "remote", "add", name, u); err != nil {
			h.t.Fatal(err)
		}
	}
	return c
}

func repoHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/repos/org/project" {
		_, _ = io.WriteString(w, repoJSON)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"message":"not found"}`)
}

func TestViewRaw(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, false, "", "repo", "view", "org/project")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, want := range []string{
		"name:\torg/project\n", "description:\tA test project\n", "visibility:\tprivate\n",
		"default_branch:\tmain\n", "fork:\ttrue\n", "parent:\tup/project\n", "stars:\t3\n",
		"open_pull_requests:\t4\n", "url:\thttps://forge/org/project\n",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, r.stdout)
		}
	}
}

func TestViewTTY(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, true, "", "repo", "view", "org/project")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, want := range []string{
		"org/project\nA test project\n", "Private · Fork of up/project", "Default branch: main",
		"Topics: cli, go", "3 stars · 1 forks · 2 open issues · 4 open pull requests",
		"View this repository on Forgejo: https://forge/org/project",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, r.stdout)
		}
	}
}

func TestViewFromGitRemote(t *testing.T) {
	h := newHarness(t, repoHandler)
	h.initGit("origin=git@HOSTNAME:org/project.git")
	r := h.run(nil, false, "", "repo", "view")
	if r.code != fjoerr.ExitOK || !strings.Contains(r.stdout, "name:\torg/project") {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestViewNotFound(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, false, "", "repo", "view", "org/missing")
	if r.code != fjoerr.ExitNotFound || !strings.Contains(r.stderr, "repository "+h.host()+"/org/missing not found") {
		t.Errorf("exit %d stderr %q", r.code, r.stderr)
	}
}

func TestViewOutsideRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t, repoHandler)
	t.Setenv("GIT_CEILING_DIRECTORIES", h.gitDir)
	r := h.run(nil, false, "", "repo", "view")
	if r.code != fjoerr.ExitUsage || !strings.Contains(r.stderr, "not in a git repository") {
		t.Errorf("exit %d stderr %q", r.code, r.stderr)
	}
	if len(h.reqs) != 0 {
		t.Errorf("unexpected requests: %v", h.reqs)
	}
}

func TestViewWeb(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, true, "", "repo", "view", "org/project", "--web", "--branch", "feature/x")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := "https://" + h.host() + "/org/project/src/branch/feature%2Fx"
	if h.opened != want {
		t.Errorf("opened %q, want %q", h.opened, want)
	}
	if !strings.Contains(r.stderr, "Opening "+want) {
		t.Errorf("stderr = %q", r.stderr)
	}
	if len(h.reqs) != 0 {
		t.Errorf("--web should not call the API: %v", h.reqs)
	}

	h.noBrowser = true
	r = h.run(nil, false, "", "repo", "view", "org/project", "-w")
	if r.code != fjoerr.ExitOK || r.stdout != "https://"+h.host()+"/org/project\n" {
		t.Errorf("no browser: exit %d stdout %q", r.code, r.stdout)
	}

	r = h.run(nil, false, "", "repo", "view", "org/project", "--branch", "main")
	if r.code != fjoerr.ExitUsage {
		t.Errorf("--branch without --web: exit %d", r.code)
	}
}

func listHandler(repos int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			_, _ = io.WriteString(w, `{"id":7,"login":"alice"}`)
		case "/api/v1/users/org":
			_, _ = io.WriteString(w, `{"id":9,"login":"org"}`)
		case "/api/v1/repos/search":
			w.Header().Set("X-Total-Count", fmt.Sprint(repos))
			var items []string
			for i := 1; i <= repos; i++ {
				items = append(items, fmt.Sprintf(`{"full_name":"org/r%d","description":"line one\nline two","private":%t,"fork":%t,"archived":%t,"updated_at":"2026-11-01T10:00:00Z"}`,
					i, i%2 == 0, i == 1, i == 3))
			}
			fmt.Fprintf(w, `{"ok":true,"data":[%s]}`, strings.Join(items, ","))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"not found"}`)
		}
	}
}

func TestListRaw(t *testing.T) {
	h := newHarness(t, listHandler(3))
	r := h.run(nil, false, "", "repo", "list", "org", "--visibility", "private", "--no-archived", "--source", "-L", "3")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := "org/r1\tline one line two\tpublic, fork\t2026-11-01T10:00:00Z\n" +
		"org/r2\tline one line two\tprivate\t2026-11-01T10:00:00Z\n" +
		"org/r3\tline one line two\tpublic, archived\t2026-11-01T10:00:00Z\n"
	if r.stdout != want {
		t.Errorf("stdout =\n%q\nwant\n%q", r.stdout, want)
	}
	q := h.reqs[len(h.reqs)-1].Query()
	for k, v := range map[string]string{
		"uid": "9", "exclusive": "true", "is_private": "true", "archived": "false",
		"mode": "source", "sort": "updated", "order": "desc", "limit": "3",
	} {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q (%s)", k, q.Get(k), v, q.Encode())
		}
	}
}

func TestListOwnRepositoriesTTY(t *testing.T) {
	h := newHarness(t, listHandler(2))
	r := h.run(nil, true, "", "repo", "list")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "Showing 2 of 2 repositories in @alice") || !strings.Contains(r.stdout, "NAME") {
		t.Errorf("stdout = %q", r.stdout)
	}
	if got := h.reqs[len(h.reqs)-1].Query().Get("uid"); got != "7" {
		t.Errorf("uid = %q", got)
	}
}

func TestListEmpty(t *testing.T) {
	h := newHarness(t, listHandler(0))
	r := h.run(nil, true, "", "repo", "list")
	if r.code != fjoerr.ExitOK || !strings.Contains(r.stderr, "No repositories") || r.stdout != "" {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestListUnknownOwner(t *testing.T) {
	h := newHarness(t, listHandler(1))
	r := h.run(nil, false, "", "repo", "list", "ghost")
	if r.code != fjoerr.ExitNotFound || !strings.Contains(r.stderr, `"ghost" not found`) {
		t.Errorf("exit %d stderr %q", r.code, r.stderr)
	}
}

func TestListUsageErrors(t *testing.T) {
	h := newHarness(t, listHandler(1))
	for _, args := range [][]string{
		{"repo", "list", "--limit", "0"},
		{"repo", "list", "--archived", "--no-archived"},
		{"repo", "list", "--fork", "--source"},
		{"repo", "list", "--visibility", "internal"},
		{"repo", "list", "a", "b"},
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

func TestSetDefault(t *testing.T) {
	h := newHarness(t, repoHandler)
	c := h.initGit(
		"origin=https://HOST/me/project.git",
		"upstream=git@HOSTNAME:org/project.git",
		"github=https://github.com/x/y.git",
	)
	ctx := context.Background()

	// Ambiguous without a terminal.
	r := h.run(nil, false, "", "repo", "set-default")
	if r.code != fjoerr.ExitUsage {
		t.Fatalf("ambiguous: exit %d stderr %q", r.code, r.stderr)
	}

	r = h.run(nil, false, "", "repo", "set-default", "org/project")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("set: exit %d stderr %q", r.code, r.stderr)
	}
	remotes, _ := c.Remotes(ctx)
	for _, rem := range remotes {
		if want := map[string]string{"upstream": "base"}[rem.Name]; rem.Resolved != want {
			t.Errorf("%s resolved = %q, want %q", rem.Name, rem.Resolved, want)
		}
	}

	r = h.run(nil, false, "", "repo", "set-default", "--view")
	if r.code != fjoerr.ExitOK || r.stdout != h.host()+"/org/project\n" {
		t.Errorf("view: exit %d stdout %q", r.code, r.stdout)
	}

	// The default is used by other commands.
	r = h.run(nil, false, "", "repo", "view")
	if r.code != fjoerr.ExitOK || !strings.Contains(r.stdout, "name:\torg/project") {
		t.Errorf("view after set-default: exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}

	// Interactive choice switches it to origin (option 1 = me/project).
	r = h.run(nil, true, "1\n", "repo", "set-default")
	if r.code != fjoerr.ExitOK || !strings.Contains(r.stderr, "Set "+h.host()+"/me/project as the default") {
		t.Fatalf("prompt: exit %d stderr %q", r.code, r.stderr)
	}
	remotes, _ = c.Remotes(ctx)
	for _, rem := range remotes {
		if want := map[string]string{"origin": "base"}[rem.Name]; rem.Resolved != want {
			t.Errorf("%s resolved = %q, want %q", rem.Name, rem.Resolved, want)
		}
	}

	r = h.run(nil, false, "", "repo", "set-default", "--unset")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("unset: exit %d stderr %q", r.code, r.stderr)
	}
	r = h.run(nil, false, "", "repo", "set-default", "--view")
	if r.code != fjoerr.ExitOK || r.stdout != "" || !strings.Contains(r.stderr, "no default repository") {
		t.Errorf("view after unset: stdout %q stderr %q", r.stdout, r.stderr)
	}
}

func TestSetDefaultSingleRemote(t *testing.T) {
	h := newHarness(t, repoHandler)
	c := h.initGit("origin=https://HOST/org/project.git")
	r := h.run(nil, false, "", "repo", "set-default")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d stderr %q", r.code, r.stderr)
	}
	remotes, _ := c.Remotes(context.Background())
	if remotes[0].Resolved != "base" {
		t.Errorf("resolved = %q", remotes[0].Resolved)
	}
}

func TestSetDefaultErrors(t *testing.T) {
	h := newHarness(t, repoHandler)
	h.initGit("origin=https://HOST/org/project.git")
	for _, args := range [][]string{
		{"repo", "set-default", "other/repo"},
		{"repo", "set-default", "--view", "--unset"},
		{"repo", "set-default", "--view", "org/project"},
	} {
		r := h.run(nil, false, "", args...)
		if r.code != fjoerr.ExitUsage {
			t.Errorf("%v: exit %d stderr %q", args, r.code, r.stderr)
		}
	}

	h2 := newHarness(t, repoHandler)
	h2.initGit("origin=https://github.com/x/y.git")
	if r := h2.run(nil, false, "", "repo", "set-default"); r.code != fjoerr.ExitUsage {
		t.Errorf("no known remote: exit %d stderr %q", r.code, r.stderr)
	}
}

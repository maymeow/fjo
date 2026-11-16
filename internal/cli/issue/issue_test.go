package issue_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
	"github.com/maymeow/fjo/internal/prompt"
)

const issueJSON = `{"id":999,"number":42,"title":"Bug","state":"open","body":"Details","user":{"login":"alice"},"html_url":"https://forge/org/project/issues/42","labels":[{"id":7,"name":"bug","color":"ff0000"}],"assignees":[{"login":"alice"}],"comments":1,"updated_at":"2026-11-01T10:00:00Z"}`

type harness struct {
	t         *testing.T
	srv       *httptest.Server
	reqs      []*url.URL
	methods   []string
	bodies    []string
	gitDir    string
	opened    string
	noBrowser bool
	prompter  prompt.Prompter
}

func issueHandler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/repos/org/project/issues":
		if r.Method == "GET" {
			io.WriteString(w, "["+issueJSON+"]")
		} else {
			io.WriteString(w, issueJSON)
		}
	case "/api/v1/repos/org/project/issues/42":
		io.WriteString(w, issueJSON)
	case "/api/v1/repos/org/project/issues/42/comments":
		if r.Method == "GET" {
			io.WriteString(w, `[{"body":"A comment","user":{"login":"bob"}}]`)
		} else {
			io.WriteString(w, `{"html_url":"https://forge/comment"}`)
		}
	case "/api/v1/repos/org/project/labels":
		io.WriteString(w, `[{"id":7,"name":"bug"},{"id":8,"name":"old"}]`)
	case "/api/v1/repos/org/project/milestones":
		io.WriteString(w, `[{"id":12,"title":"v1"}]`)
	case "/api/v1/repos/org/project/issues/42/labels", "/api/v1/repos/org/project/issues/42/labels/8", "/api/v1/repos/org/project/issues/42/pin":
		w.WriteHeader(204)
	case "/api/v1/user":
		io.WriteString(w, `{"login":"me","id":5}`)
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"message":"not found"}`)
	}
}

func TestRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"list", []string{"list"}, "#42\tBug\tbug\t2026-11-01T10:00:00Z\n"},
		{"json", []string{"list", "--json", "number,title,state,labels,assignees,milestone"}, "\"number\":42"},
		{"view", []string{"view", "#42", "--comments"}, "A comment"},
		{"viewJSON", []string{"view", "42", "--json", "number,title", "--jq", ".number"}, "42\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, issueHandler)
			args := append([]string{"issue"}, tc.args...)
			args = append(args, "-R", "org/project")
			r := h.run(nil, false, "", args...)
			if r.code != 0 || !strings.Contains(r.stdout, tc.want) {
				t.Fatalf("exit %d out %q err %q", r.code, r.stdout, r.stderr)
			}
			for _, u := range h.reqs {
				if strings.Contains(u.Path, "999") {
					t.Fatal("used internal ID")
				}
				if u.Path == "/api/v1/repos/org/project/issues" && u.Query().Get("type") != "issues" {
					t.Fatal("missing type=issues")
				}
			}
		})
	}
}

func TestMutations(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		method, path, body string
	}{
		{[]string{"create", "--title", "New", "--body-file", "-", "--label", "bug", "--assignee", "@me", "--milestone", "v1"}, "POST", "/issues", `"labels":[7]`},
		{[]string{"close", "42", "--comment", "Fixed"}, "PATCH", "/issues/42", `"state":"closed"`},
		{[]string{"reopen", "42"}, "PATCH", "/issues/42", `"state":"open"`},
		{[]string{"comment", "42", "--body-file", "-"}, "POST", "/issues/42/comments", `"body":"Hello"`},
		{[]string{"edit", "42", "--title", "Changed", "--body", "", "--add-label", "bug", "--remove-label", "old", "--add-assignee", "bob", "--remove-assignee", "alice", "--remove-milestone"}, "PATCH", "/issues/42", `"assignees":["bob"]`},
		{[]string{"delete", "42", "--yes"}, "DELETE", "/issues/42", ""},
		{[]string{"pin", "42"}, "POST", "/issues/42/pin", ""},
		{[]string{"unpin", "42"}, "DELETE", "/issues/42/pin", ""},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			h := newHarness(t, issueHandler)
			args := append([]string{"issue"}, tc.args...)
			args = append(args, "-R", "org/project")
			r := h.run(nil, false, "Hello", args...)
			if r.code != 0 {
				t.Fatalf("exit %d err %q", r.code, r.stderr)
			}
			found := false
			for i, u := range h.reqs {
				if h.methods[i] == tc.method && strings.HasSuffix(u.Path, tc.path) {
					found = true
					if !strings.Contains(h.bodies[i], tc.body) {
						t.Errorf("body %s missing %s", h.bodies[i], tc.body)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s %s", tc.method, tc.path)
			}
			if tc.args[0] == "create" && r.stdout != "https://forge/org/project/issues/42\n" {
				t.Fatalf("stdout %q", r.stdout)
			}
			if tc.args[0] == "edit" {
				all := strings.Join(h.bodies, "\n")
				if !strings.Contains(all, `"body":""`) || !strings.Contains(all, `"milestone":0`) {
					t.Fatalf("missing clear fields: %s", all)
				}
			}
		})
	}
}

func TestNonInteractiveValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code fjoerr.ExitCode
	}{
		{[]string{"create"}, fjoerr.ExitUsage}, {[]string{"delete", "42"}, fjoerr.ExitUsage},
		{[]string{"comment", "42"}, fjoerr.ExitUsage}, {[]string{"view"}, fjoerr.ExitUsage},
		{[]string{"view", "0"}, fjoerr.ExitUsage}, {[]string{"list", "--state", "merged"}, fjoerr.ExitUsage},
		{[]string{"list", "--limit", "0"}, fjoerr.ExitUsage}, {[]string{"list", "--json"}, fjoerr.ExitUsage},
		{[]string{"close", "42", "--reason", "completed"}, fjoerr.ExitUnsupported},
		{[]string{"edit", "42"}, fjoerr.ExitUsage},
		{[]string{"create", "--title", "x", "--body", "x", "--body-file", "-"}, fjoerr.ExitUsage},
		{[]string{"create", "--title", "x", "--editor"}, fjoerr.ExitUsage},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h := newHarness(t, issueHandler)
			args := append([]string{"issue"}, tc.args...)
			args = append(args, "-R", "org/project")
			r := h.run(nil, false, "", args...)
			if r.code != tc.code {
				t.Fatalf("exit %d want %d err %q", r.code, tc.code, r.stderr)
			}
			if len(h.reqs) != 0 {
				t.Fatalf("unexpected requests: %v", h.reqs)
			}
		})
	}
}

func TestFilters(t *testing.T) {
	h := newHarness(t, issueHandler)
	r := h.run(nil, false, "", "issue", "list", "-R", "org/project", "--author", "@me", "--assignee", "bob", "--label", "bug", "--milestone", "v1", "--search", "panic", "--state", "all", "-L", "3")
	if r.code != 0 {
		t.Fatal(r.stderr)
	}
	q := h.reqs[len(h.reqs)-1].Query()
	for k, v := range map[string]string{"type": "issues", "state": "all", "created_by": "me", "assigned_by": "bob", "labels": "bug", "milestones": "12", "q": "panic", "limit": "3"} {
		if q.Get(k) != v {
			t.Errorf("%s = %q want %q", k, q.Get(k), v)
		}
	}
}

func TestURLOverridesRepo(t *testing.T) {
	h := newHarness(t, issueHandler)
	r := h.run(nil, false, "", "issue", "view", h.srv.URL+"/org/project/issues/42#comment-2", "-R", "wrong/repo", "--json", "number")
	if r.code != 0 || r.stdout != "{\"number\":42}\n" {
		t.Fatalf("%d %q %q", r.code, r.stdout, r.stderr)
	}
}

func TestBrowser(t *testing.T) {
	h := newHarness(t, issueHandler)
	r := h.run(nil, false, "", "issue", "create", "-R", "org/project", "--web", "--title", "a & b", "--body", "Hello")
	if r.code != 0 || !strings.Contains(h.opened, "/org/project/issues/new?body=Hello&title=a+%26+b") || len(h.reqs) != 0 {
		t.Fatalf("%d %q %q", r.code, h.opened, r.stderr)
	}
	r = h.run(nil, false, "", "issue", "view", "42", "-R", "org/project", "--web")
	if r.code != 0 || !strings.HasSuffix(h.opened, "/issues/42") {
		t.Fatal(r.stderr, h.opened)
	}
}

func newHarness(t *testing.T, handler http.HandlerFunc) *harness {
	t.Helper()
	h := &harness{t: t, gitDir: t.TempDir()}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := *r.URL
		h.reqs = append(h.reqs, &u)
		h.methods = append(h.methods, r.Method)
		body, _ := io.ReadAll(r.Body)
		h.bodies = append(h.bodies, string(body))
		r.Body = io.NopCloser(strings.NewReader(string(body)))
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
	if h.prompter != nil {
		f.Prompter = h.prompter
	}
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

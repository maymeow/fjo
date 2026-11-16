package issue_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
)

type fakePrompt struct {
	answers []string
	calls   int
}

func (p *fakePrompt) Input(string) (string, error) {
	p.calls++
	if len(p.answers) == 0 {
		return "", fmt.Errorf("unexpected prompt")
	}
	v := p.answers[0]
	p.answers = p.answers[1:]
	return v, nil
}
func (*fakePrompt) Password(string) (string, error) {
	return "", fmt.Errorf("unexpected password prompt")
}
func (*fakePrompt) Select(string, []string) (int, error) {
	return 0, fmt.Errorf("unexpected selection prompt")
}

func TestPrompts(t *testing.T) {
	for _, tc := range []struct {
		args, answers []string
		code          fjoerr.ExitCode
		calls         int
	}{
		{[]string{"create"}, []string{"New title", "New body"}, 0, 2},
		{[]string{"comment", "42"}, []string{"Hello"}, 0, 1},
		{[]string{"delete", "42"}, []string{"42"}, 0, 1},
		{[]string{"delete", "42"}, []string{"no"}, fjoerr.ExitCancel, 1},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h := newHarness(t, issueHandler)
			p := &fakePrompt{answers: tc.answers}
			h.prompter = p
			args := append([]string{"issue"}, tc.args...)
			args = append(args, "-R", "org/project")
			r := h.run(nil, true, "", args...)
			if r.code != tc.code || p.calls != tc.calls {
				t.Fatalf("exit %d calls %d stderr %q", r.code, p.calls, r.stderr)
			}
		})
	}
	h := newHarness(t, issueHandler)
	p := &fakePrompt{}
	h.prompter = p
	r := h.run(nil, false, "", "issue", "create", "-R", "org/project")
	if r.code != fjoerr.ExitUsage || p.calls != 0 {
		t.Fatalf("noninteractive: %d calls %d", r.code, p.calls)
	}
}

func TestUnknownMetadataDoesNotMutate(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--title", "x", "--label", "missing"},
		{"edit", "42", "--title", "x", "--milestone", "missing"},
		{"list", "--label", "missing"},
		{"list", "--milestone", "missing"},
	} {
		h := newHarness(t, issueHandler)
		r := h.run(nil, false, "", append(append([]string{"issue"}, args...), "-R", "org/project")...)
		if r.code != fjoerr.ExitNotFound {
			t.Fatalf("%v: %d %s", args, r.code, r.stderr)
		}
		for _, m := range h.methods {
			if m != "GET" {
				t.Fatal("mutated on invalid metadata")
			}
		}
	}
}

func TestFailures(t *testing.T) {
	for _, action := range []string{"view", "close", "reopen", "comment", "edit", "delete", "pin", "unpin"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
				io.WriteString(w, `{"message":"forbidden"}`)
			})
			args := []string{"issue", action, "42", "-R", "org/project"}
			switch action {
			case "comment":
				args = append(args, "--body", "Hi")
			case "edit":
				args = append(args, "--title", "Hi")
			case "delete":
				args = append(args, "--yes")
			}
			r := h.run(nil, false, "", args...)
			if r.code != fjoerr.ExitForbidden {
				t.Fatalf("%d %s", r.code, r.stderr)
			}
		})
	}
}

func TestRejectPullRequest(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"number":42,"pull_request":{}}`) })
	r := h.run(nil, false, "", "issue", "close", "42", "-R", "org/project")
	if r.code != fjoerr.ExitUsage || len(h.reqs) != 1 {
		t.Fatalf("%d %s requests %d", r.code, r.stderr, len(h.reqs))
	}
}

func TestEmptyList(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `[]`) })
	r := h.run(nil, false, "", "issue", "list", "-R", "org/project")
	if r.stdout != "" || r.stderr != "no open issues in org/project\n" {
		t.Fatalf("out %q err %q", r.stdout, r.stderr)
	}
	r = h.run(nil, false, "", "issue", "list", "-R", "org/project", "--json", "number")
	if r.stdout != "[]\n" || r.stderr != "" {
		t.Fatalf("out %q err %q", r.stdout, r.stderr)
	}
}

func TestOutputGolden(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tty   bool
		flags []string
	}{
		{"list.tsv", false, nil}, {"list.txt", true, nil}, {"list.json", false, []string{"--json", "number,title,state,author,labels,assignees,milestone,comments"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, "["+strings.ReplaceAll(issueJSON, "2026-11-01T10:00:00Z", "2020-01-02T10:00:00Z")+"]")
			})
			r := h.run(nil, tc.tty, "", append([]string{"issue", "list", "-R", "org/project"}, tc.flags...)...)
			if r.code != 0 {
				t.Fatal(r.stderr)
			}
			want, err := os.ReadFile("testdata/" + tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if r.stdout != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Errorf("got:\n%s\nwant:\n%s", r.stdout, want)
			}
		})
	}
}

func TestSubpathURL(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forgejo/api/v1/repos/org/project/issues/42" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, issueJSON)
	})
	r := h.run(map[string]string{"FJO_HOST": h.host() + "/forgejo"}, false, "", "issue", "view", h.srv.URL+"/forgejo/org/project/issues/42", "--json", "number")
	if r.code != 0 || r.stdout != "{\"number\":42}\n" {
		t.Fatalf("%d %q %s", r.code, r.stdout, r.stderr)
	}
}

func TestPartialFailure(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			w.WriteHeader(403)
			io.WriteString(w, `{"message":"forbidden"}`)
			return
		}
		issueHandler(w, r)
	})
	r := h.run(nil, false, "", "issue", "close", "42", "--comment", "Closing", "-R", "org/project")
	if r.code != fjoerr.ExitForbidden || !strings.Contains(r.stderr, "comment was added") {
		t.Fatalf("%d %s", r.code, r.stderr)
	}
}

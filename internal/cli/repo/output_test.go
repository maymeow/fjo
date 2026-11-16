package repo_test

import (
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
)

func TestRepoViewJSON(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, false, "", "repo", "view", "org/project", "--json",
		"name,nameWithOwner,isPrivate,isFork,parent,stargazerCount,openPullRequestCount,topics")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := `{"isFork":true,"isPrivate":true,"name":"project","nameWithOwner":"org/project",` +
		`"openPullRequestCount":4,"parent":{"nameWithOwner":"up/project"},` +
		`"stargazerCount":3,"topics":["cli","go"]}` + "\n"
	if r.stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", r.stdout, want)
	}
}

func TestRepoViewJSONJQ(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, false, "", "repo", "view", "org/project", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if r.code != fjoerr.ExitOK || r.stdout != "org/project\n" {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestRepoViewJSONTemplate(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, false, "", "repo", "view", "org/project", "--json", "nameWithOwner", "--template", "{{.nameWithOwner}}")
	if r.code != fjoerr.ExitOK || r.stdout != "org/project" {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestRepoViewJSONWebConflict(t *testing.T) {
	h := newHarness(t, repoHandler)
	r := h.run(nil, false, "", "repo", "view", "org/project", "--json", "nameWithOwner", "--web")
	if r.code != fjoerr.ExitUsage {
		t.Fatalf("exit %d stderr %q", r.code, r.stderr)
	}
	if len(h.reqs) != 0 {
		t.Errorf("unexpected requests: %v", h.reqs)
	}
}

func TestRepoListJSON(t *testing.T) {
	h := newHarness(t, listHandler(2))
	r := h.run(nil, false, "", "repo", "list", "org", "--json",
		"nameWithOwner,isPrivate,isFork,isArchived,updatedAt")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := `[{"isArchived":false,"isFork":true,"isPrivate":false,"nameWithOwner":"org/r1","updatedAt":"2026-11-01T10:00:00Z"},` +
		`{"isArchived":false,"isFork":false,"isPrivate":true,"nameWithOwner":"org/r2","updatedAt":"2026-11-01T10:00:00Z"}]` + "\n"
	if r.stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", r.stdout, want)
	}
}

func TestRepoListJSONEmpty(t *testing.T) {
	h := newHarness(t, listHandler(0))
	r := h.run(nil, true, "", "repo", "list", "--json", "nameWithOwner")
	if r.code != fjoerr.ExitOK || r.stdout != "[]\n" {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
	if strings.Contains(r.stderr, "No repositories") {
		t.Errorf("empty message must not be printed for JSON: %q", r.stderr)
	}
}

func TestRepoListJSONErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no fields", []string{"--json"}, "Specify one or more"},
		{"unknown field", []string{"--json", "nope"}, "Unknown JSON field"},
		{"jq without json", []string{"--jq", "."}, "without --json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, listHandler(1))
			args := append([]string{"repo", "list"}, tt.args...)
			r := h.run(nil, false, "", args...)
			if r.code != fjoerr.ExitUsage || !strings.Contains(r.stderr, tt.want) {
				t.Errorf("exit %d stderr %q", r.code, r.stderr)
			}
			if len(h.reqs) != 0 {
				t.Errorf("validation must happen before the API call: %v", h.reqs)
			}
		})
	}
}

func TestRepoListJSONTTY(t *testing.T) {
	h := newHarness(t, listHandler(1))
	r := h.run(nil, true, "", "repo", "list", "org", "--json", "nameWithOwner")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "\n    \"nameWithOwner\": \"org/r1\"\n") {
		t.Errorf("expected indented JSON on a terminal, got %q", r.stdout)
	}
}

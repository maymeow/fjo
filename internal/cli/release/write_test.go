package release_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
)

func TestReleaseCreateFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("From file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := run(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == endpoint+"/tags/releases%2Fv1":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == endpoint && r.URL.Query().Get("draft") == "true":
			io.WriteString(w, "[]")
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/v1/repos/org/project/tags/releases%2Fv1":
			io.WriteString(w, `{"name":"releases/v1"}`)
		case r.Method == http.MethodPost && r.URL.Path == endpoint:
			var payload api.CreateReleaseOptions
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			want := api.CreateReleaseOptions{TagName: "releases/v1", Name: "One", Body: "From file\n", TargetCommitish: "release/branch", Draft: true, Prerelease: true, HideArchiveLinks: true}
			if payload != want {
				t.Errorf("payload = %#v, want %#v", payload, want)
			}
			io.WriteString(w, releaseJSON)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}, false, "create", "releases/v1", "-t", "One", "-F", path, "--target", "release/branch", "-d", "-p", "--verify-tag", "--hide-archive-links")
	if got.code != 0 || got.stdout != releaseURL+"\n" {
		t.Fatalf("%+v", got)
	}
}

func TestReleaseCreateRejectsInvalidInputBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code fjoerr.ExitCode
	}{
		{[]string{"create"}, fjoerr.ExitUsage},
		{[]string{"create", "../v1"}, fjoerr.ExitUsage},
		{[]string{"create", "v1", "--notes-from-tag", "-n", "note"}, fjoerr.ExitUsage},
		{[]string{"create", "v1", "--target="}, fjoerr.ExitUsage},
		{[]string{"create", "v1", "app.tar.gz"}, fjoerr.ExitUsage},
		{[]string{"edit", "v1"}, fjoerr.ExitUsage},
		{[]string{"delete", "v1"}, fjoerr.ExitUsage},
	} {
		got := run(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}, false, tc.args...)
		if got.code != tc.code {
			t.Fatalf("%v: %+v", tc.args, got)
		}
	}
}

func TestReleaseCreatePrompts(t *testing.T) {
	got := runWith(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == endpoint {
				io.WriteString(w, "[]")
				return
			}
			http.NotFound(w, r)
		case http.MethodPost:
			var payload api.CreateReleaseOptions
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.TagName != "v1" || payload.Name != "Title from prompt" || payload.Body != "" {
				t.Errorf("unexpected payload: %+v", payload)
			}
			io.WriteString(w, releaseJSON)
		default:
			t.Errorf("unexpected method %s", r.Method)
			http.NotFound(w, r)
		}
	}, true, "v1\nTitle from prompt\n", "", "create")
	if got.code != 0 || !strings.Contains(got.stderr, "Title [v1]") {
		t.Fatalf("%+v", got)
	}
}

func TestReleaseNotesFromAnnotatedTag(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	g := &git.Client{Dir: dir}
	commands := [][]string{
		{"init"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "Commit subject"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "tag.gpgSign=false", "tag", "-a", "v1", "-m", "Tag subject", "-m", "Tag body"},
		{"tag", "light"},
	}
	for _, args := range commands {
		if _, err := g.Run(t.Context(), args...); err != nil {
			t.Fatal(err)
		}
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == endpoint:
			io.WriteString(w, "[]")
		case r.Method == http.MethodGet:
			http.NotFound(w, r)
		case r.Method == http.MethodPost:
			var payload api.CreateReleaseOptions
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.TagName != "v1" || payload.Body != "Tag subject\n\nTag body" {
				t.Errorf("unexpected payload: %+v", payload)
			}
			io.WriteString(w, releaseJSON)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}
	got := runWith(t, handler, false, "", dir, "create", "v1", "--notes-from-tag")
	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	got = runWith(t, handler, false, "", dir, "create", "light", "--notes-from-tag")
	if got.code != fjoerr.ExitUsage || !strings.Contains(got.stderr, "lightweight") {
		t.Fatalf("%+v", got)
	}
}

func TestReleaseEditPatchOnlyChangedFields(t *testing.T) {
	got := runWith(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == endpoint+"/tags/releases/v1":
			io.WriteString(w, releaseJSON)
		case r.Method == http.MethodGet && r.URL.Path == endpoint+"/tags/renamed":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == endpoint:
			io.WriteString(w, "[]")
		case r.Method == http.MethodPatch && r.URL.Path == endpoint+"/9":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if len(payload) != 5 || payload["tag_name"] != "renamed" || payload["body"] != "Edited notes\n" || payload["draft"] != false ||
				payload["prerelease"] != false || payload["target_commitish"] != "stable" {
				t.Errorf("unexpected PATCH: %v", payload)
			}
			io.WriteString(w, strings.ReplaceAll(releaseJSON, "releases/v1", "renamed"))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}, false, "Edited notes\n", "", "edit", "releases/v1", "--tag", "renamed", "--draft=false", "--prerelease=false", "--target", "stable", "-F", "-")
	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestReleaseDeleteConfirmationAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, input  string
		tty, cleanup bool
		want         fjoerr.ExitCode
		tagStatus    int
	}{
		{name: "cancel", tty: true, input: "wrong\n", want: fjoerr.ExitCancel},
		{name: "release-only", tty: true, input: "releases/v1\n"},
		{name: "tag-absent", cleanup: true, tagStatus: http.StatusNotFound},
		{name: "partial-failure", cleanup: true, tagStatus: http.StatusForbidden, want: fjoerr.ExitForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"delete", "releases/v1"}
			if !tc.tty {
				args = append(args, "--yes")
			}
			if tc.cleanup {
				args = append(args, "--cleanup-tag")
			}
			got := runWith(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == endpoint+"/tags/releases/v1":
					io.WriteString(w, releaseJSON)
				case r.Method == http.MethodDelete && r.URL.Path == endpoint+"/9" && tc.want != fjoerr.ExitCancel:
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete && r.URL.EscapedPath() == "/api/v1/repos/org/project/tags/releases%2Fv1" && tc.cleanup:
					w.WriteHeader(tc.tagStatus)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}, tc.tty, tc.input, "", args...)
			if got.code != tc.want || (tc.want == fjoerr.ExitForbidden && !strings.Contains(got.stderr, "release deleted")) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

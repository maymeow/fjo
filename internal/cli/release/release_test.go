package release_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

const endpoint = "/api/v1/repos/org/project/releases"
const releaseURL = "https://forge.example/org/project/releases/tag/releases/v1"
const releaseJSON = `{
	"id":9,"tag_name":"releases/v1","name":"Version one","body":"Release notes","draft":false,"prerelease":false,
	"created_at":"2026-11-18T12:00:00Z","published_at":"2026-11-18T12:15:00Z","html_url":"` + releaseURL + `",
	"url":"https://forge.example/api/v1/repos/org/project/releases/9","tarball_url":"https://forge.example/archive.tar.gz",
	"zipball_url":"https://forge.example/archive.zip","target_commitish":"main","author":{"login":"alice"},
	"assets":[{"name":"bundle.tar.gz","size":1234,"browser_download_url":"https://forge.example/assets/archive",
	"download_count":56,"created_at":"2026-11-18T12:30:00Z"}]
}`

type result struct {
	code           fjoerr.ExitCode
	stdout, stderr string
}

func run(t *testing.T, handler http.HandlerFunc, tty bool, args ...string) result {
	t.Helper()
	return runWith(t, handler, tty, "", "", args...)
}

func runWith(t *testing.T, handler http.HandlerFunc, tty bool, input, gitDir string, args ...string) result {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	env := map[string]string{
		"FJO_CONFIG_DIR": t.TempDir(), "FJO_HOST": strings.TrimPrefix(server.URL, "https://"), "FJO_TOKEN": "test-token",
	}
	getenv := func(key string) string { return env[key] }
	streams, stdin, stdout, stderr := iostreams.Test()
	stdin.WriteString(input)
	streams.SetGetenv(getenv)
	streams.SetTTY(tty, tty, tty)
	f := app.New(streams, getenv, build.Info{Version: "test"})
	f.HTTPTransport = server.Client().Transport
	f.GitDir = gitDir
	f.Browser = func(string) error { return browser.ErrNoBrowser }
	command := append(append([]string{"release"}, args...), "-R", "org/project")
	code := cli.Run(t.Context(), f, command)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestReleaseView(t *testing.T) {
	fields := "assets,author,body,createdAt,id,isDraft,isPrerelease,name,publishedAt,tagName,tarballUrl,targetCommitish,url,zipballUrl"
	golden, err := os.ReadFile("testdata/view.json")
	if err != nil {
		t.Fatal(err)
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case endpoint + "/latest", endpoint + "/tags/releases%2Fv1":
			fmt.Fprint(w, releaseJSON)
		default:
			t.Errorf("unexpected path %s", r.URL)
			http.NotFound(w, r)
		}
	}
	got := run(t, handler, false, "view", "releases/v1", "--json", fields)
	if got.code != 0 || got.stdout != string(golden) {
		t.Fatalf("%+v", got)
	}
	got = run(t, handler, false, "view")
	if got.code != 0 || !strings.Contains(got.stdout, "Release notes") || !strings.Contains(got.stdout, "bundle.tar.gz (1234 bytes)") {
		t.Fatalf("%+v", got)
	}
	got = run(t, handler, false, "view", "-w")
	if got.code != 0 || got.stdout != releaseURL+"\n" {
		t.Fatalf("%+v", got)
	}
}

func TestDraftFallbackPaginatesAndMatchesExactTag(t *testing.T) {
	got := run(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case endpoint + "/tags/v2":
			http.NotFound(w, r)
		case endpoint:
			q := r.URL.Query()
			if q.Get("draft") != "true" {
				t.Errorf("expected draft inclusion, query = %s", r.URL.RawQuery)
			}
			if q.Get("page") == "" {
				w.Header().Set("Link", "<"+endpoint+`?draft=true&page=2>; rel="next"`)
				io.WriteString(w, `[{"tag_name":"v20","draft":true}]`)
				return
			}
			io.WriteString(w, `[{"tag_name":"v2","draft":true,"published_at":"1970-01-01T00:00:00Z"}]`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}, false, "view", "v2", "--json", "tagName,isDraft,publishedAt,assets")
	if got.code != 0 || got.stdout != "{\"assets\":[],\"isDraft\":true,\"publishedAt\":null,\"tagName\":\"v2\"}\n" {
		t.Fatalf("%+v", got)
	}
}

func TestReleaseListFiltersAndOrdersBeforeLimit(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path == endpoint+"/latest" {
			io.WriteString(w, `{"tag_name":"new"}`)
			return
		}
		if r.URL.Path != endpoint || q.Get("draft") != "false" || q.Get("pre-release") != "false" {
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
			return
		}
		if q.Get("page") == "" {
			w.Header().Set("Link", "<"+endpoint+`?draft=false&pre-release=false&page=2>; rel="next"`)
			io.WriteString(w, `[
				{"id":3,"tag_name":"pre","prerelease":true,"published_at":"2026-11-18T00:00:00Z"},
				{"id":4,"tag_name":"draft","draft":true,"created_at":"2024-11-18T00:00:00Z"},
				{"id":5,"tag_name":"new","published_at":"2026-11-17T00:00:00Z"}
			]`)
			return
		}
		io.WriteString(w, `[{"id":2,"tag_name":"old","published_at":"2025-11-18T00:00:00Z"}]`)
	}
	got := run(t, handler, false, "list", "--exclude-drafts", "--exclude-pre-releases", "-O", "asc", "-L", "1", "--json", "tagName", "--jq", ".[0].tagName")
	if got.code != 0 || got.stdout != "old\n" {
		t.Fatalf("%+v", got)
	}
	got = run(t, handler, false, "list", "--exclude-drafts", "--exclude-pre-releases", "-L", "1")
	if got.code != 0 || got.stdout != "new\tLatest\tnew\t2026-11-17T00:00:00Z\n" {
		t.Fatalf("%+v", got)
	}
	got = run(t, handler, true, "list", "--exclude-drafts", "--exclude-pre-releases", "-L", "1")
	if got.code != 0 || !strings.Contains(got.stdout, "TITLE") || !strings.Contains(got.stdout, "Latest") {
		t.Fatalf("%+v", got)
	}
}

func TestEmptyReleases(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == endpoint {
			io.WriteString(w, "[]")
			return
		}
		http.NotFound(w, r)
	}
	got := run(t, handler, false, "view")
	if got.code != fjoerr.ExitNotFound || !strings.Contains(got.stderr, "no published stable release") {
		t.Fatalf("%+v", got)
	}
	got = run(t, handler, false, "list")
	if got.code != 0 || got.stdout != "" || !strings.Contains(got.stderr, "no releases found") {
		t.Fatalf("%+v", got)
	}
	got = run(t, handler, false, "list", "--json", "tagName")
	if got.code != 0 || got.stdout != "[]\n" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
}

package apicmd_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

const testToken = "0123456789abcdef0123456789abcdef01234567"

type seen struct {
	method, path, query, auth, contentType, body string
	header                                       http.Header
}

type result struct {
	code           fjoerr.ExitCode
	stdout, stderr string
}

// server starts a TLS test server; handler sees every request after it is
// recorded in *reqs.
func server(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *[]seen) {
	t.Helper()
	var reqs []seen
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, seen{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery,
			auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"),
			body: string(b), header: r.Header.Clone(),
		})
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func run(t *testing.T, srv *httptest.Server, env map[string]string, stdin string, tty bool, args ...string) result {
	t.Helper()
	return runFull(t, srv, env, stdin, tty, t.TempDir(), args...)
}

// runIn runs with git commands executed in gitDir.
func runIn(t *testing.T, srv *httptest.Server, gitDir string, args ...string) result {
	t.Helper()
	return runFull(t, srv, nil, "", false, gitDir, args...)
}

func runFull(t *testing.T, srv *httptest.Server, env map[string]string, stdin string, tty bool, gitDir string, args ...string) result {
	t.Helper()
	e := map[string]string{"FJO_CONFIG_DIR": t.TempDir()}
	if srv != nil {
		e["FJO_HOST"] = strings.TrimPrefix(srv.URL, "https://")
		e["FJO_TOKEN"] = testToken
	}
	for k, v := range env {
		e[k] = v
	}
	ios, in, out, errOut := iostreams.Test()
	in.WriteString(stdin)
	ios.SetTTY(false, tty, false)
	getenv := func(k string) string { return e[k] }
	ios.SetGetenv(getenv)
	f := app.New(ios, getenv, build.Info{Version: "1.0.0", Platform: "test/test"})
	if srv != nil {
		f.HTTPTransport = srv.Client().Transport
	}
	f.GitDir = gitDir // never the fjo checkout itself
	code := cli.Run(context.Background(), f, args)
	return result{code, out.String(), errOut.String()}
}

func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestGetUser(t *testing.T) {
	srv, reqs := server(t, jsonHandler(200, `{"login":"alice","id":1}`))
	r := run(t, srv, nil, "", false, "api", "/user")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if r.stdout != `{"login":"alice","id":1}` {
		t.Errorf("stdout = %q", r.stdout)
	}
	got := (*reqs)[0]
	if got.method != "GET" || got.path != "/api/v1/user" || got.auth != "token "+testToken {
		t.Errorf("request = %+v", got)
	}
	if ua := got.header.Get("User-Agent"); ua != "fjo/1.0.0 (test/test)" {
		t.Errorf("User-Agent = %q", ua)
	}
}

func TestPrettyPrintOnTTY(t *testing.T) {
	srv, _ := server(t, jsonHandler(200, `{"a":[1,2]}`))
	r := run(t, srv, nil, "", true, "api", "user")
	if want := "{\n  \"a\": [\n    1,\n    2\n  ]\n}\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
}

func TestFieldsGetQuery(t *testing.T) {
	srv, reqs := server(t, jsonHandler(200, `[]`))
	r := run(t, srv, nil, "", false, "api", "/repos/search?sort=updated", "-X", "GET", "-f", "q=fjo cli", "-F", "limit=5", "-F", "private=true")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	got := (*reqs)[0]
	if got.method != "GET" || got.body != "" {
		t.Errorf("request = %+v", got)
	}
	if got.query != "sort=updated&limit=5&private=true&q=fjo+cli" {
		t.Errorf("query = %q", got.query)
	}
}

func TestFieldsPostJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "desc.txt")
	if err := os.WriteFile(file, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, reqs := server(t, jsonHandler(201, `{"id":7}`))
	r := run(t, srv, nil, "stdin text", false, "api", "/user/repos",
		"-F", "name=demo", "-F", "private=true", "-F", "size=42", "-F", "template=null",
		"-f", "raw=true", "-F", "description=@"+file, "-F", "readme=@-",
		"-F", "labels[]=1", "-F", "labels[]=2", "-f", "meta[owner]=alice")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	got := (*reqs)[0]
	if got.method != "POST" || got.contentType != "application/json" {
		t.Fatalf("request = %+v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatalf("body %q: %v", got.body, err)
	}
	want := map[string]any{
		"name": "demo", "private": true, "size": float64(42), "template": nil,
		"raw": "true", "description": "from file", "readme": "stdin text",
		"labels": []any{float64(1), float64(2)},
		"meta":   map[string]any{"owner": "alice"},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v\nwant %#v", body, want)
	}
}

func TestInputBody(t *testing.T) {
	srv, reqs := server(t, jsonHandler(200, `{}`))
	r := run(t, srv, nil, `{"x":1}`, false, "api", "-X", "PATCH", "/repos/o/r", "--input", "-", "-f", "q=1", "-H", "X-Test: yes")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	got := (*reqs)[0]
	if got.method != "PATCH" || got.body != `{"x":1}` || got.query != "q=1" || got.header.Get("X-Test") != "yes" {
		t.Errorf("request = %+v", got)
	}
	if got.contentType != "application/json" {
		t.Errorf("Content-Type = %q", got.contentType)
	}
}

func TestInputDefaultsToPost(t *testing.T) {
	srv, reqs := server(t, jsonHandler(200, `{}`))
	r := run(t, srv, nil, "data", false, "api", "/x", "--input", "-", "-H", "Content-Type: text/plain")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := (*reqs)[0]; got.method != "POST" || got.contentType != "text/plain" {
		t.Errorf("request = %+v", got)
	}
}

func TestErrorStatus(t *testing.T) {
	tests := []struct {
		status int
		code   fjoerr.ExitCode
	}{
		{401, fjoerr.ExitAuth},
		{403, fjoerr.ExitForbidden},
		{404, fjoerr.ExitNotFound},
		{422, fjoerr.ExitConflict},
		{500, fjoerr.ExitUnavailable},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			body := `{"message":"boom"}`
			srv, _ := server(t, jsonHandler(tt.status, body))
			r := run(t, srv, nil, "", false, "api", "-X", "POST", "/x")
			if r.code != tt.code {
				t.Errorf("exit %d, want %d (stderr %q)", r.code, tt.code, r.stderr)
			}
			if r.stdout != body {
				t.Errorf("stdout = %q", r.stdout)
			}
			if !strings.Contains(r.stderr, "boom") || !strings.Contains(r.stderr, fmt.Sprintf("HTTP %d", tt.status)) {
				t.Errorf("stderr = %q", r.stderr)
			}
		})
	}
}

func TestSilentAndInclude(t *testing.T) {
	srv, _ := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Total-Count", "3")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	r := run(t, srv, nil, "", false, "api", "/x", "--silent")
	if r.code != fjoerr.ExitOK || r.stdout != "" {
		t.Errorf("--silent: exit %d stdout %q", r.code, r.stdout)
	}
	r = run(t, srv, nil, "", false, "api", "/x", "-i")
	if !strings.HasPrefix(r.stdout, "HTTP/1.1 200 OK\n") || !strings.Contains(r.stdout, "X-Total-Count: 3\n") ||
		!strings.HasSuffix(r.stdout, "\n\n{\"ok\":true}") {
		t.Errorf("--include stdout = %q", r.stdout)
	}
}

func TestPaginate(t *testing.T) {
	var srvURL string
	srv, reqs := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/items?page=2>; rel="next"`, srvURL))
			_, _ = io.WriteString(w, `[{"n":1},{"n":2}]`)
		case "2":
			w.Header().Set("Link", `</api/v1/items?page=3>; rel="next"`)
			_, _ = io.WriteString(w, `[{"n":3}]`)
		default:
			_, _ = io.WriteString(w, `[]`)
		}
	})
	srvURL = srv.URL
	r := run(t, srv, nil, "", false, "api", "/items", "--paginate")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if r.stdout != `[{"n":1},{"n":2},{"n":3}]` {
		t.Errorf("stdout = %q", r.stdout)
	}
	if len(*reqs) != 3 {
		t.Errorf("%d requests, want 3", len(*reqs))
	}
	for _, q := range *reqs {
		if q.auth != "token "+testToken {
			t.Errorf("page %q without token", q.query)
		}
	}
}

func TestPaginateNonArray(t *testing.T) {
	srv, _ := server(t, jsonHandler(200, `{"login":"alice"}`))
	r := run(t, srv, nil, "", false, "api", "/user", "--paginate")
	if r.code != fjoerr.ExitOK || r.stdout != `{"login":"alice"}` {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestPaginateRefusesForeignLink(t *testing.T) {
	foreign := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("foreign server contacted with Authorization %q", r.Header.Get("Authorization"))
	}))
	defer foreign.Close()
	srv, _ := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/items?page=2>; rel="next"`, foreign.URL))
		_, _ = io.WriteString(w, `[1]`)
	})
	r := run(t, srv, nil, "", false, "api", "/items", "--paginate")
	if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "outside of") {
		t.Errorf("exit %d stderr %q", r.code, r.stderr)
	}
}

func TestAbsoluteURLOutsideInstance(t *testing.T) {
	srv, reqs := server(t, jsonHandler(200, `{}`))
	r := run(t, srv, nil, "", false, "api", "https://evil.example/api/v1/user")
	if r.code != fjoerr.ExitUsage || len(*reqs) != 0 {
		t.Errorf("exit %d, %d requests, stderr %q", r.code, len(*reqs), r.stderr)
	}
}

func TestNotLoggedIn(t *testing.T) {
	r := run(t, nil, map[string]string{"FJO_HOST": "git.example.com"}, "", false, "api", "/user")
	if r.code != fjoerr.ExitAuth {
		t.Errorf("exit %d, want %d (stderr %q)", r.code, fjoerr.ExitAuth, r.stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	srv, reqs := server(t, jsonHandler(200, `{}`))
	tests := [][]string{
		{"api"},
		{"api", "/a", "/b"},
		{"api", "/x", "-f", "novalue"},
		{"api", "/x", "-f", "=v"},
		{"api", "/x", "-f", "a[]b=1"},
		{"api", "/x", "-f", "a[][b]=1"},
		{"api", "/x", "-f", "a=1", "-f", "a=2"},
		{"api", "/x", "-f", "a=1", "-f", "a[b]=2"},
		{"api", "/x", "-H", "NoColon"},
		{"api", "/x", "-X", "POST", "--paginate"},
		{"api", "/x", "--paginate", "--input", "-"},
		{"api", "/x", "--input", "-", "-F", "a=@-"},
		{"api", "/x", "--input", filepath.Join(t.TempDir(), "missing")},
		{"api", "/repos/{owner}/{repo}"}, // not in a repository and no FJO_REPO
	}
	for _, args := range tests {
		r := run(t, srv, nil, "", false, args...)
		if r.code != fjoerr.ExitUsage {
			t.Errorf("%v: exit %d, want %d (stderr %q)", args, r.code, fjoerr.ExitUsage, r.stderr)
		}
	}
	if len(*reqs) != 0 {
		t.Errorf("%d requests sent for invalid usage", len(*reqs))
	}
}

func TestPlaceholdersFromFJORepo(t *testing.T) {
	srv, reqs := server(t, jsonHandler(200, `{}`))
	host := strings.TrimPrefix(srv.URL, "https://")
	r := run(t, srv, map[string]string{"FJO_REPO": host + "/my-org/my.repo"}, "", false, "api", "/repos/{owner}/{repo}/issues")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := (*reqs)[0].path; got != "/api/v1/repos/my-org/my.repo/issues" {
		t.Errorf("path = %q", got)
	}
}

func TestPlaceholderBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	srv, reqs := server(t, jsonHandler(200, `{}`))
	host := strings.TrimPrefix(srv.URL, "https://")
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "feature/x"},
		{"remote", "add", "origin", "https://" + host + "/o/r.git"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	r := runIn(t, srv, dir, "api", "/repos/{owner}/{repo}/branches/{branch}")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := (*reqs)[0].path; got != "/api/v1/repos/o/r/branches/feature/x" {
		t.Errorf("path = %q", got)
	}
}

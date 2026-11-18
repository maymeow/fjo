package pr_test

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
	"time"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

const repoPath = "/api/v1/repos/org/project"
const sha = "0123456789abcdef0123456789abcdef01234567"
const pull = `{"id":999,"number":42,"state":"open","title":"New feature","body":"Details","user":{"login":"alice"},"base":{"ref":"main","repo":{"full_name":"org/project"}},"head":{"ref":"feature/topic","sha":"` + sha + `","repo":{"full_name":"alice/project"}}}`

func serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "token test-credential" {
			t.Errorf("unexpected request: %s %s, authenticated=%t", r.Method, r.URL, r.Header.Get("Authorization") != "")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

type result struct {
	code                 fjoerr.ExitCode
	out, stderr, browser string
}

func run(t *testing.T, server *httptest.Server, gitDir string, out io.Writer, args ...string) result {
	t.Helper()
	env := map[string]string{
		"FJO_CONFIG_DIR": t.TempDir(), "FJO_HOST": strings.TrimPrefix(server.URL, "https://"),
		"FJO_TOKEN": "test-credential", "FJO_REPO": "org/project",
	}
	getenv := func(key string) string { return env[key] }
	streams, _, stdout, stderr := iostreams.Test()
	streams.SetGetenv(getenv)
	if out != nil {
		streams.Out = out
	}
	f := app.New(streams, getenv, build.Info{Version: "test"})
	f.GitDir = gitDir
	f.HTTPTransport = server.Client().Transport
	var res result
	f.Browser = func(link string) error {
		res.browser = link
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res.code = cli.Run(ctx, f, append([]string{"pr"}, args...))
	res.out, res.stderr = stdout.String(), stderr.String()
	return res
}

func TestListAppliesLimitAfterFilters(t *testing.T) {
	for _, version := range []string{"15.0.11", "17.0.1+gitea"} {
		t.Run(version, func(t *testing.T) {
			server := serve(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				switch r.URL.Path {
				case "/api/v1/version":
					fmt.Fprintf(w, `{"version":%q}`, version)
				case repoPath + "/labels":
					io.WriteString(w, `[{"id":3,"name":"bug"},{"id":7,"name":"urgent"}]`)
				case repoPath + "/pulls":
					wantBranch := strings.HasPrefix(version, "17")
					if q.Get("state") != "closed" || q.Get("poster") != "alice" || strings.Join(q["labels"], ",") != "3,7" ||
						(q.Get("base") == "main") != wantBranch || (q.Get("head") == "feature/topic") != wantBranch {
						t.Errorf("unexpected list query %s", q.Encode())
					}
					if q.Get("page") == "" {
						q.Set("page", "2")
						w.Header().Set("Link", "<"+repoPath+"/pulls?"+q.Encode()+`>; rel="next"`)
						io.WriteString(w, `[{"number":1,"merged":false,"title":"WIP: Other"}]`)
						return
					}
					if q.Get("page") != "2" {
						t.Errorf("read beyond requested match: %s", r.URL)
					}
					io.WriteString(w, `[
						{"number":2,"merged":true,"draft":true,"base":{"ref":"main"},"head":{"ref":"feature/topic","label":"other:feature/topic"}},
						{"number":42,"merged":true,"title":"[wip] Work","base":{"ref":"main"},"head":{"ref":"feature/topic","label":"alice:feature/topic"},"assignees":[{"login":"bob"}]},
						{"number":43,"merged":true,"draft":true,"base":{"ref":"main"},"head":{"ref":"feature/topic","label":"alice:feature/topic"},"assignees":[{"login":"bob"}]}
					]`)
				default:
					http.NotFound(w, r)
				}
			})
			res := run(t, server, t.TempDir(), nil, "list", "--state", "merged", "--draft",
				"--base", "main", "--head", "alice:feature/topic", "--author", "alice", "--assignee", "bob",
				"--label", "bug", "--label", "urgent", "-L", "1", "--json", "number,state,isDraft")
			if res.code != 0 || res.out != "[{\"isDraft\":true,\"number\":42,\"state\":\"merged\"}]\n" {
				t.Fatalf("%+v", res)
			}
		})
	}
}

func TestViewByNumberURLAndBranch(t *testing.T) {
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			io.WriteString(w, `{"version":"15.0.11"}`)
		case repoPath + "/pulls":
			io.WriteString(w, "["+pull+"]")
		case repoPath + "/pulls/42":
			io.WriteString(w, pull)
		case repoPath + "/issues/42/comments":
			io.WriteString(w, `[{"user":{"login":"bob"},"body":"Looks good"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	dir := t.TempDir()
	res := run(t, server, dir, nil, "view", "#42", "--comments")
	if res.code != 0 || !strings.Contains(res.out, "Looks good") {
		t.Fatalf("%+v", res)
	}
	for _, arg := range []string{server.URL + "/org/project/pulls/42", server.URL + "/org/project/pulls/42/files", "alice:feature/topic"} {
		res := run(t, server, dir, nil, "view", arg, "--json", "number,headRepository", "--jq", ".headRepository")
		if res.code != 0 || res.out != "alice/project\n" {
			t.Fatalf("%s: %+v", arg, res)
		}
	}
	res = run(t, server, dir, nil, "view", server.URL+"/org/project/pulls/42", "-R", "org/project", "--json", "number")
	if res.code != 0 {
		t.Fatalf("URL with matching --repo: %+v", res)
	}
	res = run(t, server, dir, nil, "view", server.URL+"/org/project/pulls/42", "-R", "org/other")
	if res.code != fjoerr.ExitUsage {
		t.Fatalf("URL with different --repo: %+v", res)
	}
	res = run(t, server, dir, nil, "view", server.URL+"/org/project/pulls/x/files")
	if res.code != fjoerr.ExitUsage {
		t.Fatalf("URL without number: %+v", res)
	}
	res = run(t, server, dir, nil, "view", "42", "--web")
	if res.code != 0 || res.browser != server.URL+"/org/project/pulls/42" {
		t.Fatalf("%+v", res)
	}
	res = run(t, server, dir, nil, "list", "--web")
	link, err := url.Parse(res.browser)
	if res.code != 0 || err != nil || link.Path != "/org/project/pulls" || link.Query().Get("state") != "open" {
		t.Fatalf("%+v", res)
	}
}

func TestViewCurrentBranchUsesForkAndTrackingBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			io.WriteString(w, `{"version":"15.0.11"}`)
		case repoPath + "/pulls":
			other := strings.ReplaceAll(strings.Replace(pull, "alice/project", "org/project", 1), "\"number\":42", "\"number\":9")
			io.WriteString(w, "["+other+","+pull+"]")
		default:
			http.NotFound(w, r)
		}
	})
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	gitRun("init", "--initial-branch=local-topic")
	gitRun("-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "initial")
	gitRun("remote", "add", "my-fork", server.URL+"/alice/project.git")
	gitRun("update-ref", "refs/remotes/my-fork/feature/topic", "HEAD")
	gitRun("branch", "--set-upstream-to=my-fork/feature/topic")
	res := run(t, server, dir, nil, "view", "--json", "number", "--jq", ".number")
	if res.code != 0 || res.out != "42\n" {
		t.Fatalf("%+v", res)
	}
}

func TestDiffFilesAndPatch(t *testing.T) {
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			io.WriteString(w, `{"version":"15.0.11"}`)
		case repoPath + "/pulls":
			io.WriteString(w, "["+pull+"]")
		case repoPath + "/pulls/42/files":
			io.WriteString(w, `[{"filename":"dir/a b.txt"}]`)
		case repoPath + "/pulls/42.patch":
			if r.URL.Query().Get("binary") != "true" {
				t.Error("expected binary changes to be included")
			}
			io.WriteString(w, "-old\n+new\n")
		default:
			http.NotFound(w, r)
		}
	})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--name-only"}, "dir/a b.txt\n"},
		{[]string{"--patch"}, "-old\n+new\n"},
		{[]string{"--patch", "--color=always"}, "\x1b[31m-old\x1b[0m\n\x1b[32m+new\x1b[0m\n"},
	} {
		for _, selector := range []string{"42", "alice:feature/topic"} {
			res := run(t, server, t.TempDir(), nil, append([]string{"diff", selector}, tc.args...)...)
			if res.code != 0 || res.out != tc.want {
				t.Fatalf("%s %v: %+v", selector, tc.args, res)
			}
		}
	}
}

type streamSink struct {
	first chan struct{}
	total int
}

func (s *streamSink) Write(p []byte) (int, error) {
	if s.total == 0 {
		close(s.first)
	}
	s.total += len(p)
	return len(p), nil
}

func TestDiffStreamsBeforeServerFinishes(t *testing.T) {
	const size = 8 << 20
	const blockSize = 32 << 10
	sink := &streamSink{first: make(chan struct{})}
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != repoPath+"/pulls/42.diff" {
			http.NotFound(w, r)
			return
		}
		block := strings.Repeat("x", blockSize)
		if _, err := io.WriteString(w, block); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		select {
		case <-sink.first:
		case <-r.Context().Done():
			t.Error("diff was not streamed before the response finished")
			return
		}
		for i := blockSize; i < size; i += blockSize {
			if _, err := io.WriteString(w, block); err != nil {
				return
			}
		}
	})
	res := run(t, server, t.TempDir(), sink, "diff", "42")
	if res.code != 0 || sink.total != size {
		t.Fatalf("streamed %d bytes: %+v", sink.total, res)
	}
}

func TestChecksExitAndLatestStatus(t *testing.T) {
	for _, tc := range []struct {
		state, bucket string
		code          fjoerr.ExitCode
	}{
		{"pending", "pending", fjoerr.ExitPending},
		{"failure", "fail", fjoerr.ExitError},
		{"error", "fail", fjoerr.ExitError},
		{"success", "pass", fjoerr.ExitOK},
		{"skipped", "skipping", fjoerr.ExitOK},
	} {
		t.Run(tc.state, func(t *testing.T) {
			server := serve(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case repoPath + "/pulls/42":
					io.WriteString(w, pull)
				case repoPath + "/statuses/" + sha:
					fmt.Fprintf(w, `[{"id":1,"context":"ci","status":"failure"},{"id":3,"context":"ci","status":%q},{"id":2,"context":"ci","status":"pending"}]`, tc.state)
				default:
					http.NotFound(w, r)
				}
			})
			res := run(t, server, t.TempDir(), nil, "checks", "42", "--json", "bucket,name,state")
			want := fmt.Sprintf("[{\"bucket\":%q,\"name\":\"ci\",\"state\":%q}]\n", tc.bucket, tc.state)
			if res.code != tc.code || res.out != want || res.stderr != "" {
				t.Fatalf("%+v", res)
			}
		})
	}
}

func TestChecksWithoutStatusesExitsOne(t *testing.T) {
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case repoPath + "/pulls/42":
			io.WriteString(w, pull)
		case repoPath + "/statuses/" + sha:
			io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	})
	res := run(t, server, t.TempDir(), nil, "checks", "42")
	if res.code != fjoerr.ExitError || res.out != "" || !strings.Contains(res.stderr, "no checks reported for pull request #42") {
		t.Fatalf("text: %+v", res)
	}
	res = run(t, server, t.TempDir(), nil, "checks", "42", "--json", "name")
	if res.code != fjoerr.ExitError || res.out != "[]\n" || res.stderr != "" {
		t.Fatalf("json: %+v", res)
	}
}

func TestDebugOutputHidesTokenAndKeepsSHA(t *testing.T) {
	server := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case repoPath + "/pulls/42":
			io.WriteString(w, pull)
		case repoPath + "/statuses/" + sha:
			io.WriteString(w, `[{"id":1,"context":"ci","status":"success"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	res := run(t, server, t.TempDir(), nil, "checks", "42", "--debug")
	if res.code != 0 {
		t.Fatalf("%+v", res)
	}
	if strings.Contains(res.stderr+res.out, "test-credential") {
		t.Errorf("token leaked into debug output: %q", res.stderr)
	}
	if !strings.Contains(res.stderr, "/statuses/"+sha) {
		t.Errorf("debug output should log the request with the full SHA: %q", res.stderr)
	}
}

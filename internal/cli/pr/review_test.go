package pr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

type scriptedPrompt struct {
	inputs  []string
	choices []int
	calls   int
}

func (p *scriptedPrompt) Input(string) (string, error) {
	p.calls++
	answer := p.inputs[0]
	p.inputs = p.inputs[1:]
	return answer, nil
}

func (p *scriptedPrompt) Password(string) (string, error) { return "", nil }

func (p *scriptedPrompt) Select(string, []string) (int, error) {
	p.calls++
	choice := p.choices[0]
	p.choices = p.choices[1:]
	return choice, nil
}

type reviewServer struct {
	mu          sync.Mutex
	noReviewAPI bool
	postStatus  int
	posted      []map[string]string
}

func (s *reviewServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == repoPath+"/pulls/42":
			_, _ = w.Write([]byte(pull))
		case r.Method == http.MethodGet && r.URL.Path == repoPath+"/pulls/42/reviews":
			if s.noReviewAPI {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == repoPath+"/pulls/42/reviews":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode review: %v", err)
			}
			s.posted = append(s.posted, body)
			if s.postStatus != 0 {
				w.WriteHeader(s.postStatus)
				_, _ = w.Write([]byte(`{"message":"approve your own pull is not allowed"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":1,"state":"` + body["event"] + `"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}
}

func runReview(t *testing.T, s *reviewServer, p *scriptedPrompt, tty bool, stdin string, args ...string) result {
	t.Helper()
	server := httptest.NewTLSServer(s.handler(t))
	t.Cleanup(server.Close)
	env := map[string]string{
		"FJO_CONFIG_DIR": t.TempDir(), "FJO_HOST": strings.TrimPrefix(server.URL, "https://"),
		"FJO_TOKEN": "test-credential", "FJO_REPO": "org/project",
	}
	getenv := func(key string) string { return env[key] }
	streams, in, stdout, stderr := iostreams.Test()
	in.WriteString(stdin)
	streams.SetTTY(tty, tty, tty)
	streams.SetGetenv(getenv)
	f := app.New(streams, getenv, build.Info{Version: "test"})
	f.HTTPTransport = server.Client().Transport
	if p != nil {
		f.Prompter = p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := cli.Run(ctx, f, append([]string{"pr", "review"}, args...))
	return result{code: code, out: stdout.String(), stderr: stderr.String()}
}

func TestReviewSubmitsEventBodyAndHeadCommit(t *testing.T) {
	for _, tc := range []struct {
		args        []string
		event, body string
		message     string
	}{
		{[]string{"42", "--approve"}, "APPROVED", "", "Approved pull request #42"},
		{[]string{"42", "-a", "--body", "LGTM"}, "APPROVED", "LGTM", "Approved pull request #42"},
		{[]string{"42", "--request-changes", "--body", "Add tests"}, "REQUEST_CHANGES", "Add tests", "Requested changes to pull request #42"},
		{[]string{"#42", "-c", "--body", "Nit"}, "COMMENT", "Nit", "Reviewed pull request #42"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			s := &reviewServer{}
			res := runReview(t, s, nil, false, "", tc.args...)
			if res.code != 0 || !strings.Contains(res.stderr, tc.message) {
				t.Fatalf("exit %d, stderr %q", res.code, res.stderr)
			}
			if len(s.posted) != 1 {
				t.Fatalf("posted %d reviews", len(s.posted))
			}
			got := s.posted[0]
			if got["event"] != tc.event || got["body"] != tc.body || got["commit_id"] != sha {
				t.Errorf("review = %v", got)
			}
		})
	}
}

func TestReviewBodyFromStdin(t *testing.T) {
	s := &reviewServer{}
	res := runReview(t, s, nil, false, "From stdin\n", "42", "--comment", "--body-file", "-")
	if res.code != 0 || len(s.posted) != 1 || s.posted[0]["body"] != "From stdin\n" {
		t.Fatalf("exit %d, posted %v, stderr %q", res.code, s.posted, res.stderr)
	}
}

func TestReviewUsageErrorsDoNotPost(t *testing.T) {
	for _, args := range [][]string{
		{"42"},
		{"42", "--approve", "--comment", "--body", "x"},
		{"42", "--request-changes"},
		{"42", "--comment"},
		{"42", "--comment", "--body", "   "},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s := &reviewServer{}
			res := runReview(t, s, nil, false, "", args...)
			if res.code != fjoerr.ExitUsage || len(s.posted) != 0 {
				t.Fatalf("exit %d, posted %v, stderr %q", res.code, s.posted, res.stderr)
			}
		})
	}
}

func TestReviewPromptsForTypeAndBodyInTerminal(t *testing.T) {
	s := &reviewServer{}
	p := &scriptedPrompt{choices: []int{1}, inputs: []string{"Please rename"}}
	res := runReview(t, s, p, true, "", "42")
	if res.code != 0 || p.calls != 2 {
		t.Fatalf("exit %d, prompts %d, stderr %q", res.code, p.calls, res.stderr)
	}
	if got := s.posted[0]; got["event"] != "REQUEST_CHANGES" || got["body"] != "Please rename" {
		t.Errorf("review = %v", got)
	}
}

func TestReviewApproveInTerminalDoesNotAskForBody(t *testing.T) {
	s := &reviewServer{}
	p := &scriptedPrompt{}
	res := runReview(t, s, p, true, "", "42", "--approve")
	if res.code != 0 || p.calls != 0 || len(s.posted) != 1 {
		t.Fatalf("exit %d, prompts %d, posted %v", res.code, p.calls, s.posted)
	}
}

func TestReviewUnsupportedServer(t *testing.T) {
	s := &reviewServer{noReviewAPI: true}
	res := runReview(t, s, nil, false, "", "42", "--approve")
	if res.code != fjoerr.ExitUnsupported || len(s.posted) != 0 || !strings.Contains(res.stderr, "does not support pull request reviews") {
		t.Fatalf("exit %d, posted %v, stderr %q", res.code, s.posted, res.stderr)
	}
}

func TestReviewOwnPullRequestIsReadableConflict(t *testing.T) {
	s := &reviewServer{postStatus: http.StatusUnprocessableEntity}
	res := runReview(t, s, nil, false, "", "42", "--approve")
	if res.code != fjoerr.ExitConflict {
		t.Fatalf("exit %d, stderr %q", res.code, res.stderr)
	}
	for _, want := range []string{"could not review pull request #42", "approve your own pull is not allowed"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr %q does not contain %q", res.stderr, want)
		}
	}
	if strings.Contains(res.stderr, "test-credential") {
		t.Error("token leaked into the error output")
	}
}

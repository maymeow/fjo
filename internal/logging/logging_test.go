package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const hexToken = "0123456789abcdef0123456789abcdef01234567"

func TestRedactorPatterns(t *testing.T) {
	r := NewRedactor()
	tests := []struct {
		in      string
		mustNot string
	}{
		{"Authorization: token supersecretvalue", "supersecretvalue"},
		{"authorization=Bearer abc.def.ghi", "abc.def.ghi"},
		{`"Authorization": "token s3cr3t"`, "s3cr3t"},
		{"Proxy-Authorization: Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"GET /api/v1/user?token=abc123&limit=1", "abc123"},
		{"access_token=xyz789 more", "xyz789"},
		{"password=hunter2", "hunter2"},
		{"clone https://alice:pa55word@git.example.com/o/r.git", "pa55word"},
		{"token " + hexToken, hexToken},
		{"token abcdef0123456789abcdef", "abcdef0123456789abcdef"},
		{"Bearer A1b2C3d4E5f6G7h8I9j0", "A1b2C3d4E5f6G7h8I9j0"},
		{"token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature", "eyJhbGciOiJIUzI1NiJ9"},
	}
	for _, tt := range tests {
		got := r.String(tt.in)
		if strings.Contains(got, tt.mustNot) {
			t.Errorf("String(%q) = %q still contains secret", tt.in, got)
		}
		if !strings.Contains(got, Redacted) {
			t.Errorf("String(%q) = %q has no redaction marker", tt.in, got)
		}
	}
}

func TestRedactorKeepsHarmlessText(t *testing.T) {
	r := NewRedactor()
	for _, s := range []string{
		"GET https://git.example.com/api/v1/repos/o/r?limit=50&page=2",
		"status=200 duration=12ms",
		"commit abc1234",
		// BACKLOG B-36: ordinary words after "token" are not secrets.
		"no token provided",
		"no token was provided",
		"token required",
		"token rejected by server",
		"token settings page",
		"token verification failed",
		"bearer credentials required",
		"token abcdefghijklmnopqrst", // 20 letters, no digit: not credential-shaped
	} {
		if got := r.String(s); got != s {
			t.Errorf("String(%q) = %q, should be unchanged", s, got)
		}
	}
}

func TestRedactorRegisteredSecrets(t *testing.T) {
	r := NewRedactor()
	r.Add("short", "x", "my-opaque-secret", "my-opaque-secret-longer")
	got := r.String("a my-opaque-secret-longer b my-opaque-secret c short x")
	for _, s := range []string{"my-opaque-secret", "short"} {
		if strings.Contains(got, s) {
			t.Errorf("registered secret %q leaked: %q", s, got)
		}
	}
	if !strings.HasSuffix(got, " x") {
		t.Errorf("1-char value must not be treated as secret: %q", got)
	}
}

func TestNilRedactorStillAppliesPatterns(t *testing.T) {
	var r *Redactor
	if got := r.String("token=abcdef"); strings.Contains(got, "abcdef") {
		t.Errorf("nil redactor leaked: %q", got)
	}
}

func TestLoggerRedactsEverything(t *testing.T) {
	var buf bytes.Buffer
	r := NewRedactor()
	r.Add("env-provided-token-value")
	log := New(&buf, Debug, r)

	hdr := http.Header{"Authorization": []string{"token " + hexToken}}
	u, _ := url.Parse("https://bob:secretpw@git.example.com/api/v1/user?token=qq123")

	log.With("init", "Authorization: token pre-bound-secret").Debug(
		"request with token=inmessage",
		"url", u,
		"headers", hdr,
		"err", errors.New("failed using env-provided-token-value"),
		slog.Group("req", "auth", "Bearer grouped-s3cret-value"),
	)

	out := buf.String()
	for _, leak := range []string{hexToken, "secretpw", "qq123", "inmessage", "env-provided-token-value", "pre-bound-secret", "grouped-s3cret-value"} {
		if strings.Contains(out, leak) {
			t.Errorf("log output leaked %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "git.example.com") {
		t.Errorf("non-secret context missing:\n%s", out)
	}
	if strings.Contains(out, "time=") {
		t.Errorf("time attribute should be omitted:\n%s", out)
	}
}

func TestLoggerLevels(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, Quiet, nil).Error("nothing")
	if buf.Len() != 0 {
		t.Errorf("quiet logger wrote %q", buf.String())
	}

	buf.Reset()
	l := New(&buf, Verbose, NewRedactor())
	l.Debug("hidden")
	l.Info("shown")
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "shown") {
		t.Errorf("verbose filtering wrong: %q", buf.String())
	}
}

func TestRedactorCommitSHAAndRegisteredToken(t *testing.T) {
	r := NewRedactor()
	r.Add(hexToken)
	const sha = "abcdef0123456789abcdef0123456789abcdef01"
	got := r.String("checks for commit " + sha + " failed using " + hexToken)
	if want := "checks for commit " + sha + " failed using " + Redacted; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

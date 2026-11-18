package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maymeow/fjo/internal/fjoerr"
)

const testToken = "0123456789abcdef0123456789abcdef01234567"

func newTestClient(t *testing.T, srv *httptest.Server, basePath string) *Client {
	t.Helper()
	c, err := New(Options{
		BaseURL:   srv.URL + basePath,
		Token:     testToken,
		UserAgent: "fjo-test",
		Transport: srv.Client().Transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = func(int) time.Duration { return 0 }
	return c
}

func TestCurrentUserSendsTokenAndHeaders(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token "+testToken {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("User-Agent") != "fjo-test" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers = %v", r.Header)
		}
		fmt.Fprint(w, `{"id":1,"login":"alice","is_admin":true}`)
	}))
	defer srv.Close()

	u, err := newTestClient(t, srv, "/api/v1").CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Login != "alice" || !u.IsAdmin || u.ID != 1 {
		t.Errorf("user = %+v", u)
	}
}

func TestSubpathInstance(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forgejo/api/v1/version" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"version":"17.0.1"}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv, "/forgejo/api/v1/")
	v, err := c.ServerVersion(context.Background())
	if err != nil || v != "17.0.1" {
		t.Fatalf("version = %q, %v", v, err)
	}
	if !strings.HasSuffix(c.InstanceURL(), "/forgejo/") {
		t.Errorf("instance = %q", c.InstanceURL())
	}
}

func TestTokenNotSentOutsideScope(t *testing.T) {
	var otherAuth atomic.Value
	otherAuth.Store("unset")
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth.Store(r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"login":"x"}`)
	}))
	defer other.Close()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/forgejo/api/v1/user":
			http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
		case "/other-app/x":
			if r.Header.Get("Authorization") != "" {
				t.Error("token sent outside the instance base path")
			}
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "/forgejo/api/v1")
	// Both servers use the same test CA, so only the origin differs.
	c.http.Transport.(*authTransport).base = srv.Client().Transport
	if _, err := c.CurrentUser(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := otherAuth.Load(); got != "" {
		t.Errorf("token leaked across origins on redirect: %q", got)
	}

	// Same origin, different base path.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/other-app/x", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestRedirectDowngradeRefused(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downgraded request was sent")
	}))
	defer plain.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/api/v1/user", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv, "/api/v1").CurrentUser(context.Background())
	if !errors.Is(err, errRedirectDowngrade) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveURL(t *testing.T) {
	c, err := New(Options{BaseURL: "https://git.example.com/forgejo/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"user":                         "https://git.example.com/forgejo/api/v1/user",
		"/user":                        "https://git.example.com/forgejo/api/v1/user",
		"/repos/a/b/issues?state=open": "https://git.example.com/forgejo/api/v1/repos/a/b/issues?state=open",
		"https://git.example.com/forgejo/attachments/x": "https://git.example.com/forgejo/attachments/x",
	} {
		u, err := c.ResolveURL(in)
		if err != nil || u.String() != want {
			t.Errorf("ResolveURL(%q) = %v, %v; want %s", in, u, err, want)
		}
	}
	for _, bad := range []string{
		"https://evil.example.com/api/v1/user",
		"http://git.example.com/forgejo/api/v1/user",
		"https://git.example.com/other/api/v1/user",
	} {
		if _, err := c.ResolveURL(bad); fjoerr.KindOf(err) != fjoerr.KindUsage {
			t.Errorf("ResolveURL(%q) err = %v, want usage error", bad, err)
		}
	}
}

func TestNewRejectsBadBaseURL(t *testing.T) {
	for _, bad := range []string{"", "git.example.com", "ftp://x/api/v1", "https://u:p@x/api/v1"} {
		if _, err := New(Options{BaseURL: bad}); err == nil {
			t.Errorf("New(%q) succeeded", bad)
		}
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   fjoerr.Kind
		msg    string
	}{
		{401, `{"message":"token is required"}`, fjoerr.KindAuth, "token is required (HTTP 401)"},
		{403, `{"message":"forbidden"}`, fjoerr.KindForbidden, "forbidden"},
		{404, ``, fjoerr.KindNotFound, "Not Found (HTTP 404)"},
		{409, `{"message":"exists"}`, fjoerr.KindConflict, "exists"},
		{422, `{"message":"invalid","errors":["name is required"]}`, fjoerr.KindConflict, "name is required"},
		{413, ``, fjoerr.KindConflict, "HTTP 413"},
		{500, `<html>boom</html>`, fjoerr.KindUnavailable, "Internal Server Error (HTTP 500)"},
		{418, `teapot`, fjoerr.KindGeneral, "teapot (HTTP 418)"},
	}
	for _, tc := range cases {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-Id", "req-1")
			w.WriteHeader(tc.status)
			fmt.Fprint(w, tc.body)
		}))
		_, err := newTestClient(t, srv, "/api/v1").CurrentUser(context.Background())
		srv.Close()

		if fjoerr.KindOf(err) != tc.kind {
			t.Errorf("%d: kind = %q, want %q (%v)", tc.status, fjoerr.KindOf(err), tc.kind, err)
		}
		if !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%d: message %q does not contain %q", tc.status, err.Error(), tc.msg)
		}
		var ae *APIError
		if !errors.As(err, &ae) || ae.StatusCode != tc.status || ae.RequestID != "req-1" || ae.Method != http.MethodGet {
			t.Errorf("%d: APIError = %+v", tc.status, ae)
		}
	}
}

func TestErrorURLIsSanitized(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := newTestClient(t, srv, "/api/v1")
	_, err := c.JSON(context.Background(), "GET", "user?access_token=secret&x=1", nil, nil)
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "?") {
		t.Errorf("error must not contain the query: %v", err)
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html>`)
	}))
	defer srv.Close()
	_, err := newTestClient(t, srv, "/api/v1").CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid response") {
		t.Errorf("err = %v", err)
	}
}

func TestRetriesSafeRequests(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"login":"alice"}`)
	}))
	defer srv.Close()
	if _, err := newTestClient(t, srv, "/api/v1").CurrentUser(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestNoRetryForUnsafeRequests(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := newTestClient(t, srv, "/api/v1").JSON(context.Background(), "POST", "user/repos", map[string]string{"name": "x"}, nil)
	if fjoerr.KindOf(err) != fjoerr.KindUnavailable || calls.Load() != 1 {
		t.Errorf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestRetryGivesUp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := newTestClient(t, srv, "/api/v1").CurrentUser(context.Background())
	if fjoerr.KindOf(err) != fjoerr.KindUnavailable || calls.Load() != 3 {
		t.Errorf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestCancelledContext(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := newTestClient(t, srv, "/api/v1").CurrentUser(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c, err := New(Options{BaseURL: srv.URL + "/api/v1", MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CurrentUser(context.Background())
	if fjoerr.KindOf(err) != fjoerr.KindUnavailable || !strings.Contains(fjoerr.HintOf(err), "ca_file") {
		t.Errorf("err = %v, hint = %q", err, fjoerr.HintOf(err))
	}
}

func TestConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()
	c, err := New(Options{BaseURL: addr + "/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = func(int) time.Duration { return 0 }
	_, err = c.CurrentUser(context.Background())
	if fjoerr.KindOf(err) != fjoerr.KindUnavailable || !strings.Contains(err.Error(), "cannot reach") {
		t.Errorf("err = %v", err)
	}
}

func TestCAFile(t *testing.T) {
	if _, err := New(Options{BaseURL: "https://x/api/v1", CAFile: "does-not-exist.pem"}); err == nil {
		t.Error("missing ca_file must fail")
	}
}

func TestRetryAfter(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "x": 0, "-1": 0, "2": 2 * time.Second, "999": 10 * time.Second} {
		if got := retryAfter(in); got != want {
			t.Errorf("retryAfter(%q) = %v", in, got)
		}
	}
}

func TestSanitizeURL(t *testing.T) {
	u, _ := url.Parse("https://user:pw@h/p?token=x#frag")
	if got := SanitizeURL(u); got != "https://h/p" {
		t.Errorf("SanitizeURL = %q", got)
	}
	if SanitizeURL(nil) != "" {
		t.Error("nil URL")
	}
}

func TestEndpointExpand(t *testing.T) {
	e := Endpoint{"GET", "/repos/{owner}/{repo}/issues/{index}"}
	if got := e.Expand("a b", "c/d", "7"); got != "repos/a%20b/c%2Fd/issues/7" {
		t.Errorf("Expand = %q", got)
	}
	for _, args := range [][]string{{"a"}, {"a", "b", "c", "d"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Expand(%v) did not panic", args)
				}
			}()
			e.Expand(args...)
		}()
	}
}

func TestJSONRequestBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Type") != "application/json" || string(b) != `{"name":"x"}` {
			t.Errorf("body = %q, ct = %q", b, r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	if _, err := newTestClient(t, srv, "/api/v1").JSON(context.Background(), "POST", "user/repos", map[string]string{"name": "x"}, nil); err != nil {
		t.Fatal(err)
	}
}

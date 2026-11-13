package auth_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/credentials"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

const testToken = "0123456789abcdef0123456789abcdef01234567"

type request struct {
	method, path, auth string
}

type result struct {
	code           fjoerr.ExitCode
	stdout, stderr string
}

type harness struct {
	t       *testing.T
	srv     *httptest.Server
	dir     string
	keyring *credentials.Memory
	file    *credentials.Memory
	reqs    []request
}

func newHarness(t *testing.T, handler http.HandlerFunc) *harness {
	t.Helper()
	h := &harness{
		t:       t,
		dir:     t.TempDir(),
		keyring: credentials.NewMemory(),
		file:    credentials.NewMemory(),
	}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.reqs = append(h.reqs, request{r.Method, r.URL.Path, r.Header.Get("Authorization")})
		handler(w, r)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// host returns the host ID of the TLS test server (host:port).
func (h *harness) host() string { return strings.TrimPrefix(h.srv.URL, "https://") }

func (h *harness) run(env map[string]string, stdin string, tty bool, args ...string) result {
	return h.runWith(env, stdin, tty, nil, args...)
}

func (h *harness) runWith(env map[string]string, stdin string, tty bool, setup func(*app.Factory), args ...string) result {
	h.t.Helper()
	e := map[string]string{"FJO_CONFIG_DIR": h.dir}
	for k, v := range env {
		e[k] = v
	}
	ios, in, out, errOut := iostreams.Test()
	in.WriteString(stdin)
	ios.SetTTY(tty, tty, false)
	getenv := func(k string) string { return e[k] }
	ios.SetGetenv(getenv)
	f := app.New(ios, getenv, build.Info{Version: "1.0.0", Platform: "test/test"})
	f.HTTPTransport = h.srv.Client().Transport
	f.Credentials = &credentials.Manager{
		Keyring:  h.keyring,
		File:     h.file,
		FilePath: filepath.Join(h.dir, credentials.FileName),
	}
	if setup != nil {
		setup(f)
	}
	code := cli.Run(context.Background(), f, args)
	return result{code, out.String(), errOut.String()}
}

func (h *harness) loadConfig() *config.Config {
	h.t.Helper()
	cfg, err := config.Load(filepath.Join(h.dir, config.FileName))
	if err != nil {
		h.t.Fatalf("load config: %v", err)
	}
	return cfg
}

func (h *harness) configText() string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.dir, config.FileName))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		h.t.Fatalf("read config: %v", err)
	}
	return string(b)
}

func tokenFor(user string) string { return "token-" + user }

// seed writes a config with the given accounts and stores a keyring token for
// each of them.
func (h *harness) seed(host, active string, users ...string) *config.Config {
	h.t.Helper()
	cfg := config.New()
	for _, u := range users {
		cfg.AddUser(host, u)
		if err := h.keyring.Set(host, u, tokenFor(u)); err != nil {
			h.t.Fatal(err)
		}
	}
	cfg.Hosts[host].ActiveUser = active
	if err := config.Save(filepath.Join(h.dir, config.FileName), cfg); err != nil {
		h.t.Fatalf("save config: %v", err)
	}
	return cfg
}

func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// userHandler answers /user based on the token and /version like Forgejo.
func userHandler(tokens map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v1/user"):
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "token ")
			login, ok := tokens[tok]
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":"unauthorized"}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"login":%q,"id":1}`, login)
		case strings.HasSuffix(r.URL.Path, "/api/v1/version"):
			_, _ = io.WriteString(w, `{"version":"17.0.1"}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestLoginWithToken(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{testToken: "alice"}))
	r := h.run(nil, testToken+"\n", false,
		"auth", "login", "--hostname", h.host(), "--with-token")

	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "Logged in to "+h.host()+" account alice (Forgejo 17.0.1)") {
		t.Errorf("stderr = %q", r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q", r.stdout)
	}

	cfg := h.loadConfig()
	host := cfg.Host(h.host())
	if host == nil {
		t.Fatal("host not saved")
	}
	if host.ActiveUser != "alice" || len(host.Users) != 1 || host.Users[0] != "alice" {
		t.Errorf("host = %+v", host)
	}
	if cfg.Defaults.Host != h.host() {
		t.Errorf("default host = %q", cfg.Defaults.Host)
	}
	if got := h.keyring.Keys(); len(got) != 1 || got[0] != h.host()+"/alice" {
		t.Errorf("keyring = %v", got)
	}
	if strings.Contains(h.configText(), testToken) {
		t.Error("the token must never be written to config.yaml")
	}
	if len(h.reqs) == 0 || h.reqs[0].auth != "token "+testToken {
		t.Errorf("requests = %+v", h.reqs)
	}
}

func TestLoginInteractive(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{testToken: "alice"}))
	r := h.run(nil, h.host()+"\n"+testToken+"\n", true, "auth", "login")

	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "Forgejo host") || !strings.Contains(r.stderr, "Paste your token") {
		t.Errorf("prompts missing from stderr: %q", r.stderr)
	}
	if !strings.Contains(r.stderr, "Logged in to "+h.host()+" account alice") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestLoginStoresTokenInsecurely(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{testToken: "alice"}))
	r := h.run(nil, testToken, false,
		"auth", "login", "--hostname", h.host(), "--with-token", "--insecure-storage")

	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.keyring.Keys(); len(got) != 0 {
		t.Errorf("keyring = %v", got)
	}
	if got := h.file.Keys(); len(got) != 1 || got[0] != h.host()+"/alice" {
		t.Errorf("file = %v", got)
	}
	if !strings.Contains(r.stderr, "Token stored in plain text") || !strings.Contains(r.stderr, credentials.FileName) {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestLoginKeyringFailure(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{testToken: "alice"}))
	h.keyring.Fail = fmt.Errorf("no secret service")

	r := h.run(nil, testToken, false,
		"auth", "login", "--hostname", h.host(), "--with-token")
	if r.code != fjoerr.ExitError {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "insecure-storage") {
		t.Errorf("hint missing from stderr: %q", r.stderr)
	}
	if h.loadConfig().Host(h.host()) != nil {
		t.Error("a failed login must not change the config")
	}

	r = h.run(nil, testToken, false,
		"auth", "login", "--hostname", h.host(), "--with-token", "--insecure-storage")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("insecure login exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.file.Keys(); len(got) != 1 {
		t.Errorf("file = %v", got)
	}
}

func TestLoginRejectedToken(t *testing.T) {
	h := newHarness(t, jsonHandler(http.StatusUnauthorized, `{"message":"unauthorized"}`))
	r := h.run(nil, testToken, false,
		"auth", "login", "--hostname", h.host(), "--with-token")

	if r.code != fjoerr.ExitAuth {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "token was rejected") || !strings.Contains(r.stderr, "read:user") {
		t.Errorf("stderr = %q", r.stderr)
	}
	if len(h.keyring.Keys()) != 0 || h.loadConfig().Host(h.host()) != nil {
		t.Error("a rejected login must not store anything")
	}
}

func TestLoginRefusesEnvToken(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	r := h.run(map[string]string{"FJO_TOKEN": testToken}, "", false,
		"auth", "login", "--hostname", h.host(), "--with-token")

	if r.code != fjoerr.ExitError {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "FJO_TOKEN") || !strings.Contains(r.stderr, "unset FJO_TOKEN") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestLoginPlainHTTPRequiresOptIn(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		userHandler(map[string]string{testToken: "alice"})(w, r)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	dir := t.TempDir()

	run := func(args ...string) result {
		ios, in, out, errOut := iostreams.Test()
		in.WriteString(testToken)
		e := map[string]string{"FJO_CONFIG_DIR": dir}
		getenv := func(k string) string { return e[k] }
		f := app.New(ios, getenv, build.Info{})
		f.Credentials = &credentials.Manager{Keyring: credentials.NewMemory(), File: credentials.NewMemory()}
		code := cli.Run(context.Background(), f, args)
		return result{code, out.String(), errOut.String()}
	}

	r := run("auth", "login", "--hostname", "http://"+host, "--with-token")
	if r.code != fjoerr.ExitUsage {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "--insecure-http") || requests != 0 {
		t.Errorf("stderr = %q, requests = %d", r.stderr, requests)
	}

	r = run("auth", "login", "--hostname", "http://"+host, "--with-token", "--insecure-http")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	cfg, err := config.Load(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if h := cfg.Host(host); h == nil || !h.InsecureHTTP {
		t.Errorf("host = %+v", h)
	}
}

func TestLoginWebOpensTokenPage(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{testToken: "alice"}))
	var opened string
	r := h.runWith(nil, testToken, true, func(f *app.Factory) {
		f.Browser = func(u string) error { opened = u; return nil }
	}, "auth", "login", "--hostname", h.host(), "--web")

	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := "https://" + h.host() + "/user/settings/applications"
	if opened != want {
		t.Errorf("opened %q, want %q", opened, want)
	}
}

func TestLoginUsageErrors(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{testToken: "alice"}))
	host := h.host()

	tests := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"invalid git protocol", "", []string{"auth", "login", "--hostname", host, "--with-token", "--git-protocol", "ftp"}, "git-protocol"},
		{"token and web", "", []string{"auth", "login", "--hostname", host, "--with-token", "--web"}, "cannot be used together"},
		{"missing hostname", "", []string{"auth", "login", "--with-token"}, "--hostname is required"},
		{"missing with-token", "", []string{"auth", "login", "--hostname", host}, "--with-token is required"},
		{"empty token", "\n", []string{"auth", "login", "--hostname", host, "--with-token"}, "no token was provided"},
		{"spaces in token", "a b\n", []string{"auth", "login", "--hostname", host, "--with-token"}, "single line"},
		{"positional argument", "", []string{"auth", "login", "extra", "--hostname", host, "--with-token"}, "unknown command"},
		{"invalid host", "", []string{"auth", "login", "--hostname", "https://", "--with-token"}, "invalid host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := h.run(nil, tt.stdin, false, tt.args...)
			if r.code != fjoerr.ExitUsage {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if !strings.Contains(r.stderr, tt.want) {
				t.Errorf("stderr = %q, want %q", r.stderr, tt.want)
			}
		})
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice", "bob")

	r := h.run(nil, "", false, "auth", "logout", "--hostname", h.host(), "--user", "bob")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "Logged out of "+h.host()+" account bob") {
		t.Errorf("stderr = %q", r.stderr)
	}
	cfg := h.loadConfig()
	if got := cfg.Host(h.host()); got == nil || len(got.Users) != 1 || got.Users[0] != "alice" {
		t.Errorf("host = %+v", got)
	}
	if got := h.keyring.Keys(); len(got) != 1 || got[0] != h.host()+"/alice" {
		t.Errorf("keyring = %v", got)
	}

	r = h.run(nil, "", false, "auth", "logout", "--hostname", h.host())
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if h.loadConfig().Host(h.host()) != nil {
		t.Error("the host entry should be removed with its last user")
	}
	if h.loadConfig().Defaults.Host != "" {
		t.Error("the default host should be cleared")
	}
}

func TestLogoutSwitchesActiveUser(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice", "bob")
	r := h.run(nil, "", false, "auth", "logout", "--hostname", h.host(), "--user", "alice")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "Switched active account") {
		t.Errorf("stderr = %q", r.stderr)
	}
	if got := h.loadConfig().Host(h.host()).ActiveUser; got != "bob" {
		t.Errorf("active user = %q", got)
	}
}

func TestLogoutNotesEnvToken(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice")
	r := h.run(map[string]string{"FJO_TOKEN": testToken}, "", false, "auth", "logout", "--hostname", h.host())
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "FJO_TOKEN is still set") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestLogoutInteractiveUserChoice(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice", "bob")
	r := h.run(nil, "2\n", true, "auth", "logout", "--hostname", h.host())
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.loadConfig().Host(h.host()); len(got.Users) != 1 || got.Users[0] != "alice" {
		t.Errorf("users = %v", got.Users)
	}
}

func TestLogoutErrors(t *testing.T) {
	t.Run("no hosts", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		r := h.run(nil, "", false, "auth", "logout")
		if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "no hosts") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
	t.Run("unknown host", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		h.seed(h.host(), "alice", "alice")
		r := h.run(nil, "", false, "auth", "logout", "--hostname", "other.example.com")
		if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "not logged in") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
	t.Run("unknown user", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		h.seed(h.host(), "alice", "alice")
		r := h.run(nil, "", false, "auth", "logout", "--hostname", h.host(), "--user", "bob")
		if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "not logged in to") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
	t.Run("ambiguous user", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		h.seed(h.host(), "alice", "alice", "bob")
		r := h.run(nil, "", false, "auth", "logout", "--hostname", h.host())
		if r.code != fjoerr.ExitUsage || !strings.Contains(r.stderr, "--user") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
	t.Run("store failure", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		h.seed(h.host(), "alice", "alice")
		h.keyring.Fail = fmt.Errorf("keyring unavailable")
		r := h.run(nil, "", false, "auth", "logout", "--hostname", h.host())
		if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "cannot remove the stored token") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
}

func TestSwitch(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice", "bob", "carol")

	r := h.run(nil, "", false, "auth", "switch", "--hostname", h.host(), "--user", "bob")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.loadConfig().Host(h.host()).ActiveUser; got != "bob" {
		t.Errorf("active user = %q", got)
	}

	r = h.run(nil, "", false, "auth", "switch", "--hostname", h.host())
	if r.code != fjoerr.ExitUsage || !strings.Contains(r.stderr, "--user") {
		t.Fatalf("ambiguous switch: exit %d, stderr %q", r.code, r.stderr)
	}

	r = h.run(nil, "", false, "auth", "switch", "--hostname", h.host(), "--user", "dave")
	if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "not logged in to") || !strings.Contains(r.stderr, "auth login") {
		t.Errorf("unknown user: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestSwitchOnlyOtherUser(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice", "bob")
	r := h.run(nil, "", false, "auth", "switch", "--hostname", h.host())
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if got := h.loadConfig().Host(h.host()).ActiveUser; got != "bob" {
		t.Errorf("active user = %q", got)
	}
}

func TestSwitchNoOtherAccount(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice")
	r := h.run(nil, "", false, "auth", "switch", "--hostname", h.host())
	if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "no accounts to switch") {
		t.Errorf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestToken(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	h.seed(h.host(), "alice", "alice", "bob")

	r := h.run(nil, "", false, "auth", "token", "--hostname", h.host())
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if r.stdout != tokenFor("alice")+"\n" {
		t.Errorf("stdout = %q", r.stdout)
	}

	r = h.run(nil, "", false, "auth", "token", "--hostname", h.host(), "--user", "bob")
	if r.code != fjoerr.ExitOK || r.stdout != tokenFor("bob")+"\n" {
		t.Errorf("stdout = %q, exit %d", r.stdout, r.code)
	}

	r = h.run(map[string]string{"FJO_TOKEN": "env-token"}, "", false, "auth", "token", "--hostname", h.host())
	if r.code != fjoerr.ExitOK || r.stdout != "env-token\n" {
		t.Errorf("FJO_TOKEN: stdout = %q, exit %d", r.stdout, r.code)
	}
}

func TestTokenNotLoggedIn(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	r := h.run(nil, "", false, "auth", "token", "--hostname", h.host())
	if r.code != fjoerr.ExitAuth {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "auth login") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{
		tokenFor("alice"): "alice",
		tokenFor("bob"):   "bob",
	}))
	h.seed(h.host(), "alice", "alice", "bob")

	r := h.run(nil, "", false, "auth", "status")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, want := range []string{
		h.host() + "\n",
		"Logged in to " + h.host() + " account alice (keyring)",
		"Logged in to " + h.host() + " account bob (keyring)",
		"Active account: true",
		"Active account: false",
		"Token: ****************",
		"API: https://" + h.host() + "/api/v1",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, tokenFor("alice")) {
		t.Error("tokens must be masked unless --show-token is given")
	}

	r = h.run(nil, "", false, "auth", "status", "--active", "--show-token")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if strings.Contains(r.stdout, "account bob") {
		t.Errorf("--active printed the inactive account:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, tokenFor("alice")) {
		t.Errorf("--show-token did not print the token:\n%s", r.stdout)
	}
}

func TestStatusFailure(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{"someone-else": "alice"}))
	h.seed(h.host(), "alice", "alice")

	r := h.run(nil, "", false, "auth", "status")
	if r.code != fjoerr.ExitError {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if r.stderr != "" {
		t.Errorf("status failures are reported on stdout, stderr = %q", r.stderr)
	}
	if !strings.Contains(r.stdout, "X Failed to log in to "+h.host()+" account alice") {
		t.Errorf("stdout = %q", r.stdout)
	}
	if !strings.Contains(r.stdout, "To re-authenticate, run: fjo auth login --hostname "+h.host()) {
		t.Errorf("stdout should hint at re-authentication:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "fjo auth status") {
		t.Errorf("auth status must not tell the user to run auth status:\n%s", r.stdout)
	}
}

func TestStatusRejectsWrongAccount(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{tokenFor("alice"): "bob"}))
	h.seed(h.host(), "alice", "alice")

	r := h.run(nil, "", false, "auth", "status")
	if r.code != fjoerr.ExitError {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "token belongs to bob, not alice") {
		t.Errorf("stdout = %q", r.stdout)
	}
}

func TestStatusUsesEnvToken(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{"env-token": "carol"}))
	r := h.run(map[string]string{"FJO_TOKEN": "env-token", "FJO_HOST": h.host()}, "", false, "auth", "status")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, want := range []string{h.host(), "account carol (FJO_TOKEN)", "Active account: true"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, r.stdout)
		}
	}
}

func TestStatusMultipleHosts(t *testing.T) {
	h := newHarness(t, userHandler(map[string]string{
		tokenFor("alice"): "alice",
		tokenFor("bob"):   "bob",
	}))
	hostA := h.host()
	hostB := h.host() + "/forgejo"

	cfg := config.New()
	cfg.AddUser(hostA, "alice")
	cfg.AddUser(hostB, "bob")
	if err := config.Save(filepath.Join(h.dir, config.FileName), cfg); err != nil {
		t.Fatal(err)
	}
	for _, acct := range []struct{ host, user string }{{hostA, "alice"}, {hostB, "bob"}} {
		if err := h.keyring.Set(acct.host, acct.user, tokenFor(acct.user)); err != nil {
			t.Fatal(err)
		}
	}

	r := h.run(nil, "", false, "auth", "status")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, want := range []string{
		"Logged in to " + hostA + " account alice",
		"Logged in to " + hostB + " account bob",
		"Active account: true",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, r.stdout)
		}
	}
	if strings.Index(r.stdout, hostA) > strings.Index(r.stdout, hostB) {
		t.Errorf("hosts are not sorted:\n%s", r.stdout)
	}

	r = h.run(nil, "", false, "auth", "token", "--hostname", hostB)
	if r.code != fjoerr.ExitOK || r.stdout != tokenFor("bob")+"\n" {
		t.Errorf("token for %s: exit %d, stdout %q", hostB, r.code, r.stdout)
	}
}

func TestStatusErrors(t *testing.T) {
	t.Run("no hosts", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		r := h.run(nil, "", false, "auth", "status")
		if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "not logged in to any") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
	t.Run("host not configured", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		h.seed(h.host(), "alice", "alice")
		r := h.run(nil, "", false, "auth", "status", "--hostname", "other.example.com")
		if r.code != fjoerr.ExitError || !strings.Contains(r.stderr, "not logged in to other.example.com") {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
	t.Run("invalid hostname", func(t *testing.T) {
		h := newHarness(t, userHandler(nil))
		r := h.run(nil, "", false, "auth", "status", "--hostname", "https://")
		if r.code != fjoerr.ExitUsage {
			t.Errorf("exit %d, stderr %q", r.code, r.stderr)
		}
	})
}

func TestStatusMissingToken(t *testing.T) {
	h := newHarness(t, userHandler(nil))
	cfg := config.New()
	cfg.AddUser(h.host(), "alice")
	if err := config.Save(filepath.Join(h.dir, config.FileName), cfg); err != nil {
		t.Fatal(err)
	}

	r := h.run(nil, "", false, "auth", "status")
	if r.code != fjoerr.ExitError {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "no token") {
		t.Errorf("stdout = %q", r.stdout)
	}
}

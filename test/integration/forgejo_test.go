//go:build integration

// Package integration_test exercises fjo against real Forgejo 15 and 17
// instances. It is opt-in: scripts/test-forgejo.sh starts the containers,
// provisions a token and runs this package with -tags=integration.
package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/cli"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/credentials"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

type instance struct {
	url, token, version string
}

func configured(t *testing.T) instance {
	t.Helper()
	inst := instance{
		url:     os.Getenv("FJO_TEST_FORGEJO_URL"),
		token:   os.Getenv("FJO_TEST_FORGEJO_TOKEN"),
		version: os.Getenv("FJO_TEST_FORGEJO_VERSION"),
	}
	if inst.url == "" || inst.token == "" {
		t.Skip("set FJO_TEST_FORGEJO_URL and FJO_TEST_FORGEJO_TOKEN (or run scripts/test-forgejo.sh)")
	}
	return inst
}

func apiURL(instanceURL string) string {
	return strings.TrimRight(instanceURL, "/") + "/api/v1"
}

type result struct {
	code           fjoerr.ExitCode
	stdout, stderr string
}

// runner executes one CLI invocation against a temporary config directory,
// reusing the credential stores so state survives between invocations.
type runner struct {
	t       *testing.T
	dir     string
	keyring *credentials.Memory
	file    *credentials.File
}

func newRunner(t *testing.T) *runner {
	dir := t.TempDir()
	return &runner{
		t:       t,
		dir:     dir,
		keyring: credentials.NewMemory(),
		file:    &credentials.File{Path: filepath.Join(dir, credentials.FileName)},
	}
}

func (r *runner) run(env map[string]string, stdin string, args ...string) result {
	r.t.Helper()
	e := map[string]string{"FJO_CONFIG_DIR": r.dir}
	for k, v := range env {
		e[k] = v
	}
	ios, in, out, errOut := iostreams.Test()
	in.WriteString(stdin)
	getenv := func(k string) string { return e[k] }
	ios.SetGetenv(getenv)
	f := app.New(ios, getenv, build.Info{Version: "integration", Platform: runtime.GOOS + "/" + runtime.GOARCH})
	f.Credentials = &credentials.Manager{
		Keyring:  r.keyring,
		File:     r.file,
		FilePath: r.file.Path,
	}
	code := cli.Run(context.Background(), f, args)
	return result{code, out.String(), errOut.String()}
}

func (r *runner) config() *config.Config {
	r.t.Helper()
	cfg, err := config.Load(filepath.Join(r.dir, config.FileName))
	if err != nil {
		r.t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestClientAgainstForgejo(t *testing.T) {
	inst := configured(t)
	ctx := t.Context()

	client, err := api.New(api.Options{
		BaseURL:   apiURL(inst.url),
		Token:     inst.token,
		UserAgent: "fjo-integration-test",
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	version, err := client.ServerVersion(ctx)
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	if inst.version != "" && !strings.HasPrefix(version, inst.version) {
		t.Errorf("server version = %q, want prefix %q", version, inst.version)
	}

	user, err := client.CurrentUser(ctx)
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if user.Login == "" {
		t.Fatal("CurrentUser returned an empty login")
	}

	// /user succeeded above only because the token was sent: the same
	// request without a token must be rejected with an auth error.
	anon, err := api.New(api.Options{BaseURL: apiURL(inst.url), UserAgent: "fjo-integration-test"})
	if err != nil {
		t.Fatalf("api.New (anonymous): %v", err)
	}
	if _, err := anon.CurrentUser(ctx); fjoerr.KindOf(err) != fjoerr.KindAuth {
		t.Errorf("anonymous CurrentUser: kind %q, err %v; want %q", fjoerr.KindOf(err), err, fjoerr.KindAuth)
	}
}

func TestAuthLifecycleAgainstForgejo(t *testing.T) {
	inst := configured(t)
	r := newRunner(t)
	loginArgs := []string{
		"auth", "login",
		"--hostname", inst.url,
		"--with-token",
		"--insecure-storage",
	}
	if strings.HasPrefix(inst.url, "http://") {
		loginArgs = append(loginArgs, "--insecure-http")
	}

	res := r.run(nil, inst.token, loginArgs...)
	if res.code != fjoerr.ExitOK {
		t.Fatalf("login exit %d, stderr %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "Logged in to") || !strings.Contains(res.stderr, "Forgejo") {
		t.Errorf("login stderr = %q", res.stderr)
	}

	cfg := r.config()
	host := cfg.Host(cfg.Defaults.Host)
	if host == nil || host.ActiveUser == "" {
		t.Fatalf("host was not saved: %+v", cfg)
	}
	user := host.ActiveUser
	if strings.Contains(readFile(t, filepath.Join(r.dir, config.FileName)), inst.token) {
		t.Error("the token must never be written to config.yaml")
	}

	creds := readFile(t, r.file.Path)
	if !strings.Contains(creds, inst.token) {
		t.Error("--insecure-storage did not write the token to credentials.yaml")
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(r.file.Path)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != 0o600 {
			t.Errorf("credentials.yaml mode = %o, want 600", got)
		}
	}

	res = r.run(nil, "", "auth", "status")
	if res.code != fjoerr.ExitOK {
		t.Fatalf("status exit %d, stdout %q, stderr %q", res.code, res.stdout, res.stderr)
	}
	for _, want := range []string{user, "insecure storage", "Active account: true"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("status output missing %q:\n%s", want, res.stdout)
		}
	}
	if strings.Contains(res.stdout, inst.token) {
		t.Error("status printed the token without --show-token")
	}

	res = r.run(nil, "", "auth", "token")
	if res.code != fjoerr.ExitOK || strings.TrimSpace(res.stdout) != inst.token {
		t.Errorf("token: exit %d, stdout %q", res.code, res.stdout)
	}

	res = r.run(nil, "", "api", "/user")
	if res.code != fjoerr.ExitOK {
		t.Fatalf("api /user exit %d, stderr %q", res.code, res.stderr)
	}
	var got struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatalf("api /user output is not JSON: %v\n%s", err, res.stdout)
	}
	if got.Login != user {
		t.Errorf("api /user login = %q, want %q", got.Login, user)
	}

	res = r.run(nil, "", "auth", "logout")
	if res.code != fjoerr.ExitOK {
		t.Fatalf("logout exit %d, stderr %q", res.code, res.stderr)
	}
	if h := r.config().Host(cfg.Defaults.Host); h != nil {
		t.Errorf("host still configured after logout: %+v", h)
	}
	if _, err := os.Stat(r.file.Path); !os.IsNotExist(err) {
		t.Errorf("credentials.yaml still exists after logout: %v", err)
	}
}

func TestEnvTokenAgainstForgejo(t *testing.T) {
	inst := configured(t)
	r := newRunner(t)

	// Plain HTTP requires an explicit per-host opt-in (§7.4); create the
	// entry without logging in, then authenticate through the environment.
	hostID, err := config.NormalizeHost(inst.url)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Hosts[hostID] = &config.Host{InsecureHTTP: strings.HasPrefix(inst.url, "http://")}
	cfg.Defaults.Host = hostID
	if err := config.Save(filepath.Join(r.dir, config.FileName), cfg); err != nil {
		t.Fatal(err)
	}

	res := r.run(map[string]string{
		"FJO_HOST":  inst.url,
		"FJO_TOKEN": inst.token,
	}, "", "api", "/user")
	if res.code != fjoerr.ExitOK {
		t.Fatalf("api /user with FJO_TOKEN: exit %d, stderr %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, `"login"`) {
		t.Errorf("stdout = %q", res.stdout)
	}

	// FJO_TOKEN is never persisted.
	if strings.Contains(readFile(t, filepath.Join(r.dir, config.FileName)), inst.token) {
		t.Error("FJO_TOKEN was written to config.yaml")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

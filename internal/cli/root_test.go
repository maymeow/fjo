package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

var testInfo = build.Info{Version: "1.2.3", Commit: "abc1234", Go: "go1.test", Platform: "testos/testarch"}

type result struct {
	code   fjoerr.ExitCode
	stdout string
	stderr string
	f      *app.Factory
}

func run(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	if _, ok := env["FJO_CONFIG_DIR"]; !ok {
		env["FJO_CONFIG_DIR"] = t.TempDir()
	}
	io, _, out, errOut := iostreams.Test()
	getenv := func(k string) string { return env[k] }
	io.SetGetenv(getenv)
	f := app.New(io, getenv, testInfo)
	code := Run(context.Background(), f, args)
	return result{code, out.String(), errOut.String(), f}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"help"}} {
		r := run(t, nil, args...)
		if r.code != fjoerr.ExitOK {
			t.Fatalf("%v: exit %d, stderr %q", args, r.code, r.stderr)
		}
		if !strings.Contains(r.stdout, "Work with Forgejo") || !strings.Contains(r.stdout, "version") {
			t.Errorf("%v: unexpected help output:\n%s", args, r.stdout)
		}
	}
}

func TestVersionCommandAndFlag(t *testing.T) {
	want := "fjo version 1.2.3\ncommit: abc1234\ngo: go1.test\nplatform: testos/testarch\n"
	for _, args := range [][]string{{"version"}, {"--version"}} {
		r := run(t, nil, args...)
		if r.code != fjoerr.ExitOK {
			t.Fatalf("%v: exit %d, stderr %q", args, r.code, r.stderr)
		}
		if r.stdout != want {
			t.Errorf("%v: stdout = %q, want %q", args, r.stdout, want)
		}
		if r.stderr != "" {
			t.Errorf("%v: unexpected stderr %q", args, r.stderr)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	tests := [][]string{
		{"nosuchcommand"},
		{"--nosuchflag"},
		{"version", "extra-arg"},
		{"--color", "rainbow", "version"},
	}
	for _, args := range tests {
		r := run(t, nil, args...)
		if r.code != fjoerr.ExitUsage {
			t.Errorf("%v: exit %d, want %d (stderr %q)", args, r.code, fjoerr.ExitUsage, r.stderr)
		}
		if r.stdout != "" {
			t.Errorf("%v: errors must not go to stdout, got %q", args, r.stdout)
		}
		if !strings.HasPrefix(r.stderr, "error: ") {
			t.Errorf("%v: stderr = %q", args, r.stderr)
		}
	}
}

func TestVersionWorksWithBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("version: 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := run(t, map[string]string{"FJO_CONFIG_DIR": dir}, "version")
	if r.code != fjoerr.ExitOK {
		t.Errorf("version must not depend on config: exit %d, %q", r.code, r.stderr)
	}
}

func TestColorFlagApplied(t *testing.T) {
	r := run(t, nil, "--color", "always", "version")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !r.f.IO.ColorEnabled() {
		t.Error("--color always not applied")
	}
}

func TestDebugOutputIsRedactedAndOnStderr(t *testing.T) {
	secret := "fjo-env-secret-token-value"
	r := run(t, map[string]string{"FJO_TOKEN": secret, "FJO_HOST": "git.example.com"}, "--debug", "version")
	if r.code != fjoerr.ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "settings resolved") {
		t.Errorf("expected debug output on stderr, got %q", r.stderr)
	}
	if strings.Contains(r.stdout, "settings resolved") {
		t.Error("debug output leaked to stdout")
	}
	if strings.Contains(r.stdout+r.stderr, secret) {
		t.Error("FJO_TOKEN value leaked")
	}
}

func TestErrorMessagesAreRedacted(t *testing.T) {
	io, _, _, errOut := iostreams.Test()
	f := app.New(io, func(k string) string {
		if k == "FJO_TOKEN" {
			return "very-secret-token"
		}
		return ""
	}, testInfo)
	printError(f, fjoerr.New(fjoerr.KindGeneral, "request failed with very-secret-token"))
	if strings.Contains(errOut.String(), "very-secret-token") {
		t.Errorf("secret leaked in error: %q", errOut.String())
	}
}

func TestInterruptedExitCode(t *testing.T) {
	io, _, _, _ := iostreams.Test()
	f := app.New(io, func(k string) string {
		if k == "FJO_CONFIG_DIR" {
			return t.TempDir()
		}
		return ""
	}, testInfo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code := Run(ctx, f, []string{"nosuchcommand"})
	if code != fjoerr.ExitInterrupted {
		t.Errorf("exit = %d, want %d", code, fjoerr.ExitInterrupted)
	}
}

func TestIsTruthy(t *testing.T) {
	for _, s := range []string{"1", "true", "yes", "ON", "api"} {
		if !isTruthy(s) {
			t.Errorf("%q should be truthy", s)
		}
	}
	for _, s := range []string{"", "0", "false", "No", "off", " "} {
		if isTruthy(s) {
			t.Errorf("%q should be falsy", s)
		}
	}
}

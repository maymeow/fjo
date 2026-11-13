package browser

import (
	"errors"
	"runtime"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestCommandPrefersFJOBrowser(t *testing.T) {
	cmd := command(env(map[string]string{
		"FJO_BROWSER": "/usr/bin/fjo-browser --new-window",
		"BROWSER":     "/usr/bin/other",
	}), "https://example.com/x")
	if cmd == nil {
		t.Fatal("command = nil")
	}
	want := []string{"/usr/bin/fjo-browser", "--new-window", "https://example.com/x"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	}
}

func TestCommandFallsBackToBROWSER(t *testing.T) {
	cmd := command(env(map[string]string{"BROWSER": "my-browser"}), "https://example.com/x")
	if cmd == nil {
		t.Fatal("command = nil")
	}
	if got, want := cmd.Args[0], "my-browser"; got != want {
		t.Errorf("args[0] = %q, want %q", got, want)
	}
}

func TestCommandNoLaunchOverSSH(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY"} {
		cmd := command(env(map[string]string{key: "1"}), "https://example.com/x")
		if cmd != nil {
			t.Errorf("%s: command = %v, want nil", key, cmd.Args)
		}
	}
}

func TestCommandExplicitBrowserWorksOverSSH(t *testing.T) {
	cmd := command(env(map[string]string{
		"SSH_CONNECTION": "1",
		"FJO_BROWSER":    "my-browser",
	}), "https://example.com/x")
	if cmd == nil {
		t.Fatal("an explicit browser command should override SSH detection")
	}
}

func TestCommandLinuxDisplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific display detection")
	}

	if cmd := command(env(nil), "https://example.com/x"); cmd != nil {
		t.Errorf("no DISPLAY: command = %v, want nil", cmd.Args)
	}
	for _, key := range []string{"DISPLAY", "WAYLAND_DISPLAY"} {
		cmd := command(env(map[string]string{key: "1"}), "https://example.com/x")
		if cmd == nil {
			t.Fatalf("%s: command = nil", key)
		}
		if got := cmd.Args[0]; got != "xdg-open" {
			t.Errorf("%s: args[0] = %q, want xdg-open", key, got)
		}
	}
}

func TestNewRejectsNonHTTPURLs(t *testing.T) {
	open := New(env(nil))
	for _, u := range []string{
		"javascript:alert(1)",
		"file:///etc/passwd",
		"ftp://example.com/x",
		"://bad",
		"",
	} {
		if err := open(u); err == nil {
			t.Errorf("open(%q) succeeded, want an error", u)
		}
	}
}

func TestNewReturnsErrNoBrowser(t *testing.T) {
	open := New(env(map[string]string{"SSH_CONNECTION": "1"}))
	if err := open("https://example.com/x"); !errors.Is(err, ErrNoBrowser) {
		t.Errorf("err = %v, want ErrNoBrowser", err)
	}
}

func TestNewStartsConfiguredBrowser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix helper command")
	}
	open := New(env(map[string]string{"FJO_BROWSER": "true"}))
	if err := open("https://example.com/x"); err != nil {
		t.Errorf("open: %v", err)
	}
}

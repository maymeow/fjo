// Package browser opens URLs in the user's web browser.
//
// FJO_BROWSER or BROWSER select a custom command. Over SSH (no local display)
// nothing is launched and the caller prints the URL instead (BACKLOG B-29).
package browser

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoBrowser means no browser can be launched in this environment.
var ErrNoBrowser = errors.New("no browser available")

// Opener opens a URL.
type Opener func(rawURL string) error

// New returns an opener using getenv to read FJO_BROWSER, BROWSER and the
// SSH session variables.
func New(getenv func(string) string) Opener {
	return func(rawURL string) error {
		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return errors.New("refusing to open a non-http(s) URL")
		}
		cmd := command(getenv, u.String())
		if cmd == nil {
			return ErrNoBrowser
		}
		return cmd.Start()
	}
}

func command(getenv func(string) string, u string) *exec.Cmd {
	for _, v := range []string{"FJO_BROWSER", "BROWSER"} {
		if b := strings.TrimSpace(getenv(v)); b != "" {
			fields := strings.Fields(b)
			// Argument array, never a shell string (§8.3).
			return exec.Command(fields[0], append(fields[1:], u)...)
		}
	}
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		return exec.Command("open", u)
	default:
		if getenv("WSL_DISTRO_NAME") != "" {
			if p, err := exec.LookPath("wslview"); err == nil {
				return exec.Command(p, u)
			}
		}
		if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
			return nil
		}
		return exec.Command("xdg-open", u)
	}
}

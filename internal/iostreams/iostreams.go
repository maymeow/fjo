// Package iostreams abstracts standard streams and terminal capabilities so
// commands never touch os.Stdin/os.Stdout directly and can be tested.
package iostreams

import (
	"bytes"
	"io"
	"os"

	"golang.org/x/term"
)

// ColorMode is the user's color preference.
type ColorMode string

// Color modes accepted by --color and config output.color.
const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// ParseColorMode validates a color mode string.
func ParseColorMode(s string) (ColorMode, bool) {
	switch ColorMode(s) {
	case ColorAuto, ColorAlways, ColorNever:
		return ColorMode(s), true
	}
	return "", false
}

// IOStreams bundles the process streams with terminal detection results.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdinTTY  bool
	stdoutTTY bool
	stderrTTY bool

	colorMode ColorMode
	getenv    func(string) string
}

// System returns streams bound to the real process.
func System() *IOStreams {
	return &IOStreams{
		In:        os.Stdin,
		Out:       os.Stdout,
		ErrOut:    os.Stderr,
		stdinTTY:  isTerminal(os.Stdin),
		stdoutTTY: isTerminal(os.Stdout),
		stderrTTY: isTerminal(os.Stderr),
		colorMode: ColorAuto,
		getenv:    os.Getenv,
	}
}

// Test returns streams backed by buffers, all non-TTY, with an empty
// environment. The returned buffers are stdin, stdout and stderr.
func Test() (*IOStreams, *bytes.Buffer, *bytes.Buffer, *bytes.Buffer) {
	in, out, errOut := &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	return &IOStreams{
		In:        in,
		Out:       out,
		ErrOut:    errOut,
		colorMode: ColorAuto,
		getenv:    func(string) string { return "" },
	}, in, out, errOut
}

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// SetTTY overrides terminal detection (tests).
func (s *IOStreams) SetTTY(stdin, stdout, stderr bool) {
	s.stdinTTY, s.stdoutTTY, s.stderrTTY = stdin, stdout, stderr
}

// SetGetenv overrides environment lookup (tests).
func (s *IOStreams) SetGetenv(fn func(string) string) { s.getenv = fn }

// SetColorMode sets the resolved color preference.
func (s *IOStreams) SetColorMode(m ColorMode) { s.colorMode = m }

// IsStdinTTY reports whether stdin is a terminal.
func (s *IOStreams) IsStdinTTY() bool { return s.stdinTTY }

// IsStdoutTTY reports whether stdout is a terminal.
func (s *IOStreams) IsStdoutTTY() bool { return s.stdoutTTY }

// IsStderrTTY reports whether stderr is a terminal.
func (s *IOStreams) IsStderrTTY() bool { return s.stderrTTY }

// CanPrompt reports whether interactive prompts are allowed: both stdin and
// stdout must be terminals and FJO_PROMPT_DISABLED must be unset.
func (s *IOStreams) CanPrompt() bool {
	if s.getenv("FJO_PROMPT_DISABLED") != "" {
		return false
	}
	return s.stdinTTY && s.stdoutTTY
}

// ColorEnabled reports whether stdout output should be colored.
//
// Precedence: explicit always/never > NO_COLOR / FJO_NO_COLOR >
// CLICOLOR_FORCE > terminal detection.
func (s *IOStreams) ColorEnabled() bool {
	switch s.colorMode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}
	if s.getenv("NO_COLOR") != "" || s.getenv("FJO_NO_COLOR") != "" {
		return false
	}
	if v := s.getenv("CLICOLOR_FORCE"); v != "" && v != "0" {
		return true
	}
	return s.stdoutTTY && s.getenv("TERM") != "dumb"
}

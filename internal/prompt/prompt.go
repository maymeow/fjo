// Package prompt implements the few interactive prompts fjo needs. Callers
// must check IOStreams.CanPrompt first; prompts never run without a terminal.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

// Prompter asks the user for input.
type Prompter interface {
	// Input reads one line of visible text.
	Input(message string) (string, error)
	// Password reads one line without echo.
	Password(message string) (string, error)
	// Select returns the index of the chosen option.
	Select(message string, options []string) (int, error)
}

// Terminal prompts on the process terminal through IOStreams.
type Terminal struct {
	IO *iostreams.IOStreams
	r  *bufio.Reader
}

// New returns a terminal prompter.
func New(io *iostreams.IOStreams) *Terminal { return &Terminal{IO: io} }

func (t *Terminal) reader() *bufio.Reader {
	if t.r == nil {
		t.r = bufio.NewReader(t.IO.In)
	}
	return t.r
}

func (t *Terminal) readLine() (string, error) {
	line, err := t.reader().ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		if errors.Is(err, io.EOF) {
			return "", fjoerr.New(fjoerr.KindCancelled, "input cancelled")
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Input implements Prompter.
func (t *Terminal) Input(message string) (string, error) {
	fmt.Fprintf(t.IO.ErrOut, "? %s ", message)
	return t.readLine()
}

// Password implements Prompter. On a real terminal echo is disabled.
func (t *Terminal) Password(message string) (string, error) {
	fmt.Fprintf(t.IO.ErrOut, "? %s ", message)
	if f, ok := t.IO.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(t.IO.ErrOut)
		if err != nil {
			return "", fjoerr.Wrap(fjoerr.KindCancelled, err, "input cancelled")
		}
		return string(b), nil
	}
	return t.readLine()
}

// Select implements Prompter with a numbered list.
func (t *Terminal) Select(message string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, errors.New("nothing to select")
	}
	fmt.Fprintf(t.IO.ErrOut, "? %s\n", message)
	for i, o := range options {
		fmt.Fprintf(t.IO.ErrOut, "  %d) %s\n", i+1, o)
	}
	for {
		fmt.Fprintf(t.IO.ErrOut, "  Enter a number (1-%d): ", len(options))
		line, err := t.readLine()
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
	}
}

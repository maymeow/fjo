package output

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// Editor is a resolved editor command.
type Editor struct {
	Command string
	Args    []string
}

// ResolveEditor applies the documented precedence (BACKLOG B-28):
// FJO_EDITOR > config editor > VISUAL > EDITOR > platform default.
func ResolveEditor(getenv func(string) string, configEditor string) Editor {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	for _, v := range []string{getenv("FJO_EDITOR"), configEditor, getenv("VISUAL"), getenv("EDITOR")} {
		if strings.TrimSpace(v) != "" {
			return parseEditor(v)
		}
	}
	if runtime.GOOS == "windows" {
		return parseEditor("notepad")
	}
	return parseEditor("nano")
}

func parseEditor(s string) Editor {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return Editor{}
	}
	return Editor{Command: fields[0], Args: fields[1:]}
}

// String returns the command line, used in diagnostics.
func (e Editor) String() string {
	return strings.TrimSpace(strings.Join(append([]string{e.Command}, e.Args...), " "))
}

// EditorOptions supplies the process streams and lets tests replace the
// actual editor invocation (BACKLOG B-28 / PHASE3 3a tests).
type EditorOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Run executes the editor with the file path appended to its arguments.
	// Nil runs a child process.
	Run func(ctx context.Context, command string, args ...string) error
}

// Edit opens the editor on a private temporary file and returns its contents.
// An editor that leaves the file empty cancels the operation (exit 2).
func (e Editor) Edit(ctx context.Context, initial string, o EditorOptions) (edited string, err error) {
	if e.Command == "" {
		return "", fjoerr.New(fjoerr.KindGeneral, "no editor configured").
			WithHint("set FJO_EDITOR, VISUAL or EDITOR")
	}
	f, err := os.CreateTemp("", "fjo-*.md")
	if err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "create temporary file")
	}
	path := f.Name()
	defer func() {
		if rmErr := os.Remove(path); rmErr != nil {
			err = fjoerr.JoinCleanup(err, fjoerr.Wrap(fjoerr.KindGeneral, rmErr, "remove temporary file"))
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, errors.Join(err, f.Close()), "secure temporary file")
	}
	if _, err := io.WriteString(f, initial); err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, errors.Join(err, f.Close()), "write temporary file")
	}
	if err := f.Close(); err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "close temporary file")
	}

	args := append(append([]string{}, e.Args...), path)
	if o.Run != nil {
		if err := o.Run(ctx, e.Command, args...); err != nil {
			return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "editor %q failed", e.Command)
		}
	} else {
		cmd := exec.CommandContext(ctx, e.Command, args...)
		cmd.Stdin = o.Stdin
		cmd.Stdout = o.Stdout
		cmd.Stderr = o.Stderr
		if cmd.Stdin == nil {
			cmd.Stdin = os.Stdin
		}
		if cmd.Stdout == nil {
			cmd.Stdout = os.Stdout
		}
		if cmd.Stderr == nil {
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "editor %q failed", e.Command)
		}
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: path is the temporary file created above
	if err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "read edited file")
	}
	body := string(data)
	if strings.TrimSpace(body) == "" {
		return "", fjoerr.New(fjoerr.KindCancelled, "empty body, aborting")
	}
	return body, nil
}

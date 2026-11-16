package output

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// DefaultPager is used when neither the environment nor config sets one.
const DefaultPager = "less -FRX"

// Pager is a resolved pager command.
type Pager struct {
	Command string
	Args    []string
}

// ResolvePager applies the documented precedence (BACKLOG B-28):
// FJO_PAGER > config pager > PAGER > "less -FRX".
func ResolvePager(getenv func(string) string, configPager string) Pager {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	for _, v := range []string{getenv("FJO_PAGER"), configPager, getenv("PAGER")} {
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "auto") {
			continue
		}
		return parsePager(v)
	}
	return parsePager(DefaultPager)
}

func parsePager(s string) Pager {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		fields = strings.Fields(DefaultPager)
	}
	return Pager{Command: fields[0], Args: fields[1:]}
}

// String returns the command line, used in diagnostics.
func (p Pager) String() string {
	return strings.TrimSpace(strings.Join(append([]string{p.Command}, p.Args...), " "))
}

// PagerOptions controls whether a pager may be started.
type PagerOptions struct {
	// Disabled corresponds to --no-pager.
	Disabled bool
	// Machine disables the pager for --json/--jq/--template output.
	Machine bool
}

// Run calls fn with a writer that feeds the pager. The pager is skipped when
// stdout is not a terminal, when it is disabled, or when the pager executable
// does not exist (BACKLOG B-28); fn then writes directly to out.
func (p Pager) Run(ctx context.Context, out, errOut io.Writer, o PagerOptions, fn func(io.Writer) error) error {
	if o.Disabled || o.Machine || !isTerminalWriter(out) || p.Command == "" {
		return fn(out)
	}
	if _, err := exec.LookPath(p.Command); err != nil {
		return fn(out)
	}
	cmd := exec.CommandContext(ctx, p.Command, p.Args...)
	cmd.Stdout = out
	cmd.Stderr = errOut
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fn(out)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fn(out)
	}
	writeErr := fn(stdin)
	_ = stdin.Close()
	waitErr := cmd.Wait()
	if writeErr != nil && !isBrokenPipe(writeErr) {
		return writeErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A pager that exits non-zero (for example, because the user quit) is
	// not a failure of the command that produced the output.
	_ = waitErr
	return nil
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func isBrokenPipe(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "broken pipe") || strings.Contains(s, "pipe closed")
}

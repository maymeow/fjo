package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

// AddRepoFlag registers -R/--repo with the gh wording.
func AddRepoFlag(cmd *cobra.Command, repo *string) {
	cmd.Flags().StringVarP(repo, "repo", "R", "", "Select a repository: [HOST/]OWNER/REPO")
}

// JSONOptions returns the --json/--jq/--template write options for the
// current terminal.
func JSONOptions(f *app.Factory) output.WriteOptions {
	return output.WriteOptions{TTY: f.IO.IsStdoutTTY(), Color: f.IO.ColorEnabled(), Width: f.IO.TerminalWidth()}
}

// TableOptions returns table printer options for the current terminal.
func TableOptions(f *app.Factory) output.TableOptions {
	return output.TableOptions{TTY: f.IO.IsStdoutTTY(), Color: f.IO.ColorEnabled(), Width: f.IO.TerminalWidth()}
}

// Page runs write through the configured pager unless disabled is set or
// stdout is not a terminal.
func Page(ctx context.Context, f *app.Factory, disabled bool, write func(io.Writer) error) error {
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	pager := output.ResolvePager(f.Getenv, cfg.Defaults.Pager)
	return pager.Run(ctx, f.IO.Out, f.IO.ErrOut, output.PagerOptions{Disabled: disabled}, write)
}

// OpenBrowser opens u for --web. It announces the URL on a terminal, prints
// it to stdout when no browser is available, and otherwise fails with a hint.
func OpenBrowser(f *app.Factory, u string) error {
	if f.IO.IsStderrTTY() {
		if _, err := fmt.Fprintf(f.IO.ErrOut, "Opening %s in your browser.\n", u); err != nil {
			return err
		}
	}
	err := f.Browser(u)
	if errors.Is(err, browser.ErrNoBrowser) {
		_, err = fmt.Fprintln(f.IO.Out, u)
		return err
	}
	if err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot open the browser").
			WithHint("open " + u + " manually or set FJO_BROWSER")
	}
	return nil
}

// OneLine collapses whitespace and strips terminal control sequences so
// server text fits in one table cell or header line.
func OneLine(s string) string {
	return output.Sanitize(strings.Join(strings.Fields(s), " "))
}

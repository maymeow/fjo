package repo

import (
	"fmt"
	"strings"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

// confirmAction asks for confirmation unless --yes was given. Destructive
// commands refuse to run without --yes when prompts are unavailable.
func confirmAction(f *app.Factory, yes bool, action, subject string) error {
	if yes {
		return nil
	}
	if !f.IO.CanPrompt() {
		return fjoerr.Usage("%s requires --yes when not running interactively", action)
	}
	answer, err := f.Prompter.Input(fmt.Sprintf("%s %s? (y/N)", action, subject))
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return fjoerr.New(fjoerr.KindCancelled, "not confirmed")
}

// reportRepo prints the URL on stdout and a short confirmation on stderr when
// the output is a terminal (scripts get a stable URL).
func reportRepo(f *app.Factory, action string, r *api.Repository) {
	if f.IO.IsStderrTTY() && r.FullName != "" {
		fmt.Fprintf(f.IO.ErrOut, "✓ %s %s\n", action, r.FullName)
	}
	if r.HTMLURL != "" {
		fmt.Fprintln(f.IO.Out, r.HTMLURL)
	}
}

// reportName prints a stable result for commands whose response has no URL.
func reportName(f *app.Factory, action, name string) {
	if f.IO.IsStderrTTY() {
		fmt.Fprintf(f.IO.ErrOut, "✓ %s %s\n", action, name)
	}
}

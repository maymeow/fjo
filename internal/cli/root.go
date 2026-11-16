// Package cli defines the cobra command tree. Commands stay thin: they parse
// input, call services and format output (IMPLEMENTATION §3.1).
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
	apicmd "github.com/maymeow/fjo/internal/cli/api"
	"github.com/maymeow/fjo/internal/cli/auth"
	"github.com/maymeow/fjo/internal/cli/issue"
	"github.com/maymeow/fjo/internal/cli/repo"
	"github.com/maymeow/fjo/internal/cli/version"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
	"github.com/maymeow/fjo/internal/logging"
)

// Main runs fjo with the process environment and returns the exit code.
func Main(args []string) fjoerr.ExitCode {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	f := app.New(iostreams.System(), os.Getenv, build.Get())
	return Run(ctx, f, args)
}

// Run executes the command tree with an injected factory (used by tests).
func Run(ctx context.Context, f *app.Factory, args []string) fjoerr.ExitCode {
	root := NewRootCmd(f)
	root.SetArgs(args)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return fjoerr.ExitOK
	}
	if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
		err = fjoerr.Wrap(fjoerr.KindInterrupted, err, "interrupted")
	}
	printError(f, err)
	return fjoerr.ExitCodeOf(err)
}

func printError(f *app.Factory, err error) {
	if fjoerr.IsSilent(err) {
		return
	}
	msg := f.Redactor.String(err.Error())
	fmt.Fprintf(f.IO.ErrOut, "error: %s\n", msg)
	if hint := fjoerr.HintOf(err); hint != "" {
		fmt.Fprintf(f.IO.ErrOut, "hint: %s\n", f.Redactor.String(hint))
	}
}

type globalFlags struct {
	color   string
	verbose bool
	debug   bool
}

// NewRootCmd builds the root command and registers all subcommands.
func NewRootCmd(f *app.Factory) *cobra.Command {
	var flags globalFlags

	cmd := &cobra.Command{
		Use:           "fjo <command> <subcommand> [flags]",
		Short:         "Forgejo CLI",
		Long:          "Work with Forgejo from the command line.",
		Example:       "  fjo version\n  fjo --help",
		Version:       f.BuildInfo.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return setup(f, cmd, flags)
		},
	}

	cmd.SetIn(f.IO.In)
	cmd.SetOut(f.IO.Out)
	cmd.SetErr(f.IO.ErrOut)
	cmd.SetVersionTemplate(version.Format(f.BuildInfo))

	pf := cmd.PersistentFlags()
	pf.StringVar(&flags.color, "color", "", `Use color in output: "auto", "always" or "never"`)
	pf.BoolVar(&flags.verbose, "verbose", false, "Show progress details on stderr")
	pf.BoolVar(&flags.debug, "debug", false, "Show sanitized diagnostic details on stderr (implies --verbose)")

	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fjoerr.Usage("%s", err.Error()).WithHint("run 'fjo --help' for usage")
	})
	// Unknown subcommands of the root are usage errors, not general failures.
	cmd.Args = func(c *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fjoerr.Usage("unknown command %q for %q", args[0], c.CommandPath()).
				WithHint("run 'fjo --help' for a list of commands")
		}
		return nil
	}
	cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }

	cmd.AddCommand(version.NewCmd(f), auth.NewCmd(f), repo.NewCmd(f), issue.NewCmd(f), apicmd.NewCmd(f))

	classifyArgErrors(cmd)
	return cmd
}

// classifyArgErrors wraps every command's positional-argument validator so
// that cobra's untyped errors (e.g. from cobra.NoArgs) become usage errors.
func classifyArgErrors(cmd *cobra.Command) {
	if orig := cmd.Args; orig != nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			err := orig(c, args)
			if err == nil {
				return nil
			}
			var fe *fjoerr.Error
			if errors.As(err, &fe) {
				return err
			}
			return fjoerr.Usage("%s", err.Error()).
				WithHint(fmt.Sprintf("run '%s --help' for usage", c.CommandPath()))
		}
	}
	for _, sub := range cmd.Commands() {
		classifyArgErrors(sub)
	}
}

// setup applies global flags and environment to the factory before any
// command runs.
func setup(f *app.Factory, cmd *cobra.Command, flags globalFlags) error {
	if flags.color != "" {
		if _, ok := iostreams.ParseColorMode(flags.color); !ok {
			return fjoerr.Usage("invalid value %q for --color: use auto, always or never", flags.color)
		}
	}

	level := logging.Quiet
	if flags.verbose {
		level = logging.Verbose
	}
	if flags.debug || isTruthy(f.Getenv("FJO_DEBUG")) {
		level = logging.Debug
	}
	f.Logger = logging.New(f.IO.ErrOut, level, f.Redactor)

	// Commands that do not need configuration still work when it is broken,
	// so a bad config never blocks `fjo version` or `fjo --help`.
	cfg, cfgErr := f.Config()
	if cfgErr != nil && needsConfig(cmd) {
		return cfgErr
	}
	if cfgErr != nil {
		f.Logger.Debug("configuration not loaded", "err", cfgErr)
	}

	s := config.ResolveSettings(cfg, config.Overrides{Color: flags.color}, f.Getenv)
	mode, _ := iostreams.ParseColorMode(s.Color.Value)
	f.IO.SetColorMode(mode)

	f.Logger.Debug("settings resolved",
		"color", s.Color.Value, "color_source", string(s.Color.Source),
		"host", s.Host.Value, "host_source", string(s.Host.Source))
	return nil
}

// annotationNoConfig marks commands that must work without a valid config.
const annotationNoConfig = "fjo/no-config"

func needsConfig(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[annotationNoConfig] == "true" {
			return false
		}
	}
	return cmd.HasParent() // root alone (help) never needs config
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

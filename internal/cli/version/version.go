// Package version implements `fjo version`.
package version

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/build"
)

// Format renders build information in the format defined in IMPLEMENTATION §15.
func Format(info build.Info) string {
	var b strings.Builder
	fmt.Fprintf(&b, "fjo version %s\n", info.Version)
	fmt.Fprintf(&b, "commit: %s\n", info.Commit)
	if info.Date != "" {
		fmt.Fprintf(&b, "built: %s\n", info.Date)
	}
	fmt.Fprintf(&b, "go: %s\n", info.Go)
	fmt.Fprintf(&b, "platform: %s\n", info.Platform)
	return b.String()
}

// NewCmd returns the version command.
func NewCmd(f *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show fjo version information",
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			"fjo/no-config": "true",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(f.IO.Out, Format(f.BuildInfo))
			return err
		},
	}
}

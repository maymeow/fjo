// Package cmdutil holds helpers shared by command packages.
package cmdutil

import (
	"github.com/spf13/cobra"
)

// AnnotationNoConfig marks commands that must work without a valid config.
const AnnotationNoConfig = "fjo/no-config"

// AddHostnameFlag registers --hostname (gh name) plus a hidden --host alias
// (IMPLEMENTATION §5.2 examples). With short=true "-h" is the shorthand, as
// in `gh auth`; cobra then keeps only the long --help on that command.
func AddHostnameFlag(cmd *cobra.Command, p *string, short bool) {
	usage := "Forgejo host: host ID or URL (e.g. git.example.com, example.com/forgejo)"
	if short {
		// Define --help without a shorthand first; cobra then does not try
		// to register -h for help (it would panic on the clash).
		cmd.Flags().Bool("help", false, "Show help for command")
		cmd.Flags().StringVarP(p, "hostname", "h", "", usage)
	} else {
		cmd.Flags().StringVar(p, "hostname", "", usage)
	}
	cmd.Flags().StringVar(p, "host", "", "Alias for --hostname")
	if err := cmd.Flags().MarkHidden("host"); err != nil {
		panic(err)
	}
}

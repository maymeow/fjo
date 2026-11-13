// Package repo implements `fjo repo` (IMPLEMENTATION §5.3).
package repo

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/reporef"
)

// NewCmd returns the `repo` command group.
func NewCmd(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo <command>",
		Short: "Work with Forgejo repositories",
		Long: `Work with Forgejo repositories.

Commands that take a [HOST/]OWNER/REPO argument use the repository of the
current directory when it is omitted: FJO_REPO, then the default chosen with
'fjo repo set-default', then the git remote that points to a known Forgejo
host. Several remotes pointing to different repositories are an error.`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newViewCmd(f),
		newSetDefaultCmd(f),
	)
	return cmd
}

// repoArg resolves an optional repository argument.
func repoArg(ctx context.Context, f *app.Factory, args []string) (reporef.Ref, error) {
	if len(args) > 0 {
		return f.ParseRepo(args[0])
	}
	return f.ResolveRepo(ctx, "")
}

// webURL builds a browser URL for a path on the repository's instance.
func webURL(f *app.Factory, ref reporef.Ref, path ...string) string {
	scheme := "https"
	if cfg, err := f.Config(); err == nil {
		if h := cfg.Host(ref.Host); h != nil {
			if h.InsecureHTTP {
				scheme = "http"
			}
			if h.APIURL != "" {
				if base, ok := strings.CutSuffix(strings.TrimRight(h.APIURL, "/"), "/api/v1"); ok {
					return base + "/" + strings.Join(append([]string{ref.FullName()}, path...), "/")
				}
			}
		}
	}
	return scheme + "://" + ref.Host + "/" + strings.Join(append([]string{ref.FullName()}, path...), "/")
}

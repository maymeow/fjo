// Package repo implements `fjo repo` (IMPLEMENTATION §5.3).
package repo

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
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
host. Several remotes pointing to different repositories are an error.
Single-repository commands also accept -R/--repo, which takes precedence.`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newViewCmd(f),
		newCreateCmd(f),
		newCloneCmd(f),
		newForkCmd(f),
		newEditCmd(f),
		newRenameCmd(f),
		newDeleteCmd(f),
		newArchiveCmd(f),
		newUnarchiveCmd(f),
		newSyncCmd(f),
		newSetDefaultCmd(f),
	)
	return cmd
}

// repoArgWithFlag resolves an optional repository argument and the -R/--repo
// override (IMPLEMENTATION §8.2: --repo first, then FJO_REPO, set-default and
// git remotes).
func repoArgWithFlag(ctx context.Context, f *app.Factory, flag string, args []string) (reporef.Ref, error) {
	if flag != "" {
		if len(args) > 0 {
			return reporef.Ref{}, fjoerr.Usage("--repo cannot be combined with a repository argument")
		}
		return f.ResolveRepo(ctx, flag)
	}
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

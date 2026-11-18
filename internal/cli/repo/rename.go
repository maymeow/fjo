package repo

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type renameOptions struct {
	repo string
	yes  bool
}

func newRenameCmd(f *app.Factory) *cobra.Command {
	var o renameOptions
	cmd := &cobra.Command{
		Use:   "rename <new-name>",
		Short: "Rename a repository",
		Long: `Rename a repository.

With one argument, the repository is resolved from --repo, FJO_REPO, the
default set with 'fjo repo set-default', or the git remotes. The two-argument
form 'rename <repository> <new-name>' selects it explicitly.`,
		Example: `  fjo repo rename new-name
  fjo repo rename my-org/project project-archive --yes`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename(cmd.Context(), f, o, args)
		},
	}
	cmdutil.AddRepoFlag(cmd, &o.repo)
	cmd.Flags().BoolVar(&o.yes, "yes", false, "Confirm the rename without prompting")
	return cmd
}

func runRename(ctx context.Context, f *app.Factory, o renameOptions, args []string) error {
	var targetArgs []string
	newName := args[0]
	if len(args) == 2 {
		targetArgs = args[:1]
		newName = args[1]
	}
	if strings.TrimSpace(newName) == "" || strings.Contains(newName, "/") {
		return fjoerr.Usage("invalid repository name %q: use a single name without a slash", newName)
	}
	ref, err := repoArgWithFlag(ctx, f, o.repo, targetArgs)
	if err != nil {
		return err
	}
	if err := confirmAction(f, o.yes, "Rename", ref.String()+" to "+newName); err != nil {
		return err
	}
	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	repo, err := client.EditRepository(ctx, ref.Owner, ref.Name, api.EditRepoOptions{Name: &newName})
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found", ref).
				WithHint("check the name and that your account can rename it")
		}
		return err
	}
	reportRepo(f, "Renamed repository", repo)
	return nil
}

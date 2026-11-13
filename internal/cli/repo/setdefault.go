package repo

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type setDefaultOptions struct {
	view  bool
	unset bool
}

func newSetDefaultCmd(f *app.Factory) *cobra.Command {
	var o setDefaultOptions
	cmd := &cobra.Command{
		Use:   "set-default [<repository>]",
		Short: "Configure the default repository for the current directory",
		Long: `Choose which repository fjo commands use in this clone when several git
remotes point to different Forgejo repositories (for example a fork and its
upstream).

The choice is stored in the local git config as remote.<name>.fjo-resolved
(like gh's gh-resolved) and is never shared. The repository must be one of
the git remotes.`,
		Example: `  fjo repo set-default
  fjo repo set-default upstream-org/project
  fjo repo set-default --view
  fjo repo set-default --unset`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetDefault(cmd.Context(), f, o, args)
		},
	}
	cmd.Flags().BoolVarP(&o.view, "view", "v", false, "View the current default repository")
	cmd.Flags().BoolVarP(&o.unset, "unset", "u", false, "Unset the current default repository")
	return cmd
}

func runSetDefault(ctx context.Context, f *app.Factory, o setDefaultOptions, args []string) error {
	if o.view && o.unset {
		return fjoerr.Usage("--view and --unset cannot be used together")
	}
	if (o.view || o.unset) && len(args) > 0 {
		return fjoerr.Usage("a repository argument cannot be combined with --view or --unset")
	}

	git := f.Git()
	remotes, err := git.Remotes(ctx)
	if err != nil {
		return app.GitContextError(err)
	}

	if o.unset {
		if err := git.UnsetResolved(ctx); err != nil {
			return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot unset the default repository")
		}
		if f.IO.IsStderrTTY() {
			fmt.Fprintln(f.IO.ErrOut, "✓ Unset the default repository")
		}
		return nil
	}

	candidates, err := f.RemoteRepos(ctx, remotes)
	if err != nil {
		return err
	}

	if o.view {
		for _, r := range remotes {
			if r.Resolved == "" {
				continue
			}
			if r.Resolved != "base" {
				fmt.Fprintln(f.IO.Out, r.Resolved)
				return nil
			}
			for _, c := range candidates {
				if c.Remote.Name == r.Name {
					fmt.Fprintln(f.IO.Out, c.Repo.String())
					return nil
				}
			}
		}
		fmt.Fprintln(f.IO.ErrOut, "no default repository has been set; use 'fjo repo set-default' to select one")
		return nil
	}

	unique := app.DistinctRepos(candidates)
	var chosen app.RemoteRepo
	switch {
	case len(args) > 0:
		ref, err := f.ParseRepo(args[0])
		if err != nil {
			return err
		}
		found := false
		for _, c := range unique {
			if c.Repo.Equal(ref) {
				chosen, found = c, true
				break
			}
		}
		if !found {
			return fjoerr.Usage("%s does not correspond to any git remote", ref).
				WithHint("add it with 'git remote add <name> <url>' first")
		}
	case len(unique) == 0:
		return fjoerr.Usage("no git remote points to a known Forgejo host").
			WithHint("log in with 'fjo auth login' or add a remote")
	case len(unique) == 1:
		chosen = unique[0]
	case !f.IO.CanPrompt():
		return fjoerr.Usage("several remotes point to different repositories; specify one").
			WithHint("fjo repo set-default OWNER/REPO")
	default:
		opts := make([]string, len(unique))
		for i, c := range unique {
			opts[i] = fmt.Sprintf("%s (%s)", c.Repo, c.Remote.Name)
		}
		i, err := f.Prompter.Select("Which repository should be the default?", opts)
		if err != nil {
			return err
		}
		chosen = unique[i]
	}

	if err := git.UnsetResolved(ctx); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot update the default repository")
	}
	if err := git.SetResolved(ctx, chosen.Remote.Name, "base"); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot set the default repository")
	}
	if f.IO.IsStderrTTY() {
		fmt.Fprintf(f.IO.ErrOut, "✓ Set %s as the default repository for the current directory\n", chosen.Repo)
	}
	return nil
}

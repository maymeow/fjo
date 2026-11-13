package repo

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type forkOptions struct {
	repo  string
	org   string
	name  string
	clone bool
}

func newForkCmd(f *app.Factory) *cobra.Command {
	var o forkOptions
	cmd := &cobra.Command{
		Use:   "fork [<repository>]",
		Short: "Fork a repository",
		Long: `Fork a repository into your account or an organization.

With --clone the fork is cloned into a new directory and the source repository
is added as the "upstream" remote.`,
		Example: `  fjo repo fork my-org/project
  fjo repo fork my-org/project --org my-team --clone
  fjo repo fork --repo my-org/project --name project-fork`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFork(cmd.Context(), f, o, args)
		},
	}
	addRepoFlag(cmd, &o.repo)
	cmd.Flags().StringVar(&o.org, "org", "", "Organization to fork into (default: your account)")
	cmd.Flags().StringVar(&o.name, "name", "", "Name of the fork (default: the source name)")
	cmd.Flags().BoolVar(&o.clone, "clone", false, "Clone the fork and add the source as the upstream remote")
	return cmd
}

func runFork(ctx context.Context, f *app.Factory, o forkOptions, args []string) error {
	source, err := repoArgWithFlag(ctx, f, o.repo, args)
	if err != nil {
		return err
	}
	client, _, err := f.ClientFor(source.Host)
	if err != nil {
		return err
	}
	sourceRepo, err := client.GetRepository(ctx, source.Owner, source.Name)
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found", source).
				WithHint("check the name and that your account can access it")
		}
		return err
	}

	fork, err := client.ForkRepository(ctx, source.Owner, source.Name, api.ForkRepoOptions{
		Name:         o.name,
		Organization: o.org,
	})
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindConflict {
			return fjoerr.Wrap(fjoerr.KindConflict, err, "a fork of %s already exists", source)
		}
		return err
	}
	if fork.FullName == "" {
		return fjoerr.New(fjoerr.KindGeneral, "%s did not return the created fork", source.Host)
	}

	if o.clone {
		dir, err := cloneRepository(ctx, f, source.Host, fork, "", nil)
		if err != nil {
			return fjoerr.Wrap(fjoerr.KindOf(err), err, "fork %s was created but could not be cloned", fork.FullName).
				WithHint("clone it manually with 'fjo repo clone " + fork.FullName + "'")
		}
		if err := addUpstream(ctx, f, source.Host, sourceRepo, dir); err != nil {
			return err
		}
		if f.IO.IsStderrTTY() {
			fmt.Fprintf(f.IO.ErrOut, "✓ Cloned %s to %s\n", fork.FullName, dir)
		}
	}
	reportRepo(f, "Forked repository", fork)
	return nil
}

// addUpstream adds the source repository as the "upstream" remote of a fork
// clone (gh-compatible).
func addUpstream(ctx context.Context, f *app.Factory, host string, source *api.Repository, dir string) error {
	u, err := cloneURL(f, host, source)
	if err != nil {
		return err
	}
	g := f.Git()
	g.Dir = dir
	if _, err := g.Run(ctx, "remote", "add", "upstream", u); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot add the upstream remote in %s", dir).
			WithHint("add it manually with 'git remote add upstream " + u + "'")
	}
	return nil
}

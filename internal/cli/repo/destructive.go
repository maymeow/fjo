package repo

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type deleteOptions struct {
	repo string
	yes  bool
}

func newDeleteCmd(f *app.Factory) *cobra.Command {
	var o deleteOptions
	cmd := &cobra.Command{
		Use:   "delete [<repository>]",
		Short: "Delete a repository",
		Long:  "Delete a repository and everything in it. This cannot be undone.",
		Example: `  fjo repo delete my-org/old-project
  fjo repo delete --repo my-org/old-project --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), f, o, args)
		},
	}
	addRepoFlag(cmd, &o.repo)
	cmd.Flags().BoolVar(&o.yes, "yes", false, "Confirm the deletion without prompting")
	return cmd
}

func runDelete(ctx context.Context, f *app.Factory, o deleteOptions, args []string) error {
	ref, err := repoArgWithFlag(ctx, f, o.repo, args)
	if err != nil {
		return err
	}
	if err := confirmAction(f, o.yes, "Delete repository", ref.String()); err != nil {
		return err
	}
	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	if err := client.DeleteRepository(ctx, ref.Owner, ref.Name); err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found", ref)
		}
		return err
	}
	reportName(f, "Deleted repository", ref.String())
	return nil
}

type archiveOptions struct {
	repo string
	yes  bool
}

func newArchiveCmd(f *app.Factory) *cobra.Command {
	var o archiveOptions
	cmd := &cobra.Command{
		Use:   "archive [<repository>]",
		Short: "Archive a repository",
		Long:  "Archive a repository. Archived repositories are read-only until unarchived.",
		Example: `  fjo repo archive my-org/old-project
  fjo repo archive --repo my-org/old-project --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArchive(cmd.Context(), f, o, args, true)
		},
	}
	addRepoFlag(cmd, &o.repo)
	cmd.Flags().BoolVar(&o.yes, "yes", false, "Confirm archiving without prompting")
	return cmd
}

func newUnarchiveCmd(f *app.Factory) *cobra.Command {
	var o archiveOptions
	cmd := &cobra.Command{
		Use:     "unarchive [<repository>]",
		Short:   "Unarchive a repository",
		Long:    "Make an archived repository writable again.",
		Example: `  fjo repo unarchive my-org/old-project`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArchive(cmd.Context(), f, o, args, false)
		},
	}
	addRepoFlag(cmd, &o.repo)
	return cmd
}

func runArchive(ctx context.Context, f *app.Factory, o archiveOptions, args []string, archive bool) error {
	ref, err := repoArgWithFlag(ctx, f, o.repo, args)
	if err != nil {
		return err
	}
	if archive {
		if err := confirmAction(f, o.yes, "Archive repository", ref.String()); err != nil {
			return err
		}
	}
	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	value := archive
	repo, err := client.EditRepository(ctx, ref.Owner, ref.Name, api.EditRepoOptions{Archived: &value})
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found", ref)
		}
		return err
	}
	action := "Archived repository"
	if !archive {
		action = "Unarchived repository"
	}
	reportRepo(f, action, repo)
	return nil
}

type syncOptions struct {
	repo   string
	branch string
}

func newSyncCmd(f *app.Factory) *cobra.Command {
	var o syncOptions
	cmd := &cobra.Command{
		Use:   "sync [<repository>]",
		Short: "Sync a fork with its upstream repository",
		Long: `Update a fork from its upstream repository.

Without --branch only the default branch is synced.`,
		Example: `  fjo repo sync
  fjo repo sync my-org/project
  fjo repo sync --repo my-org/project --branch develop`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), f, o, args)
		},
	}
	addRepoFlag(cmd, &o.repo)
	cmd.Flags().StringVarP(&o.branch, "branch", "b", "", "Branch to sync (default: the default branch)")
	return cmd
}

func runSync(ctx context.Context, f *app.Factory, o syncOptions, args []string) error {
	ref, err := repoArgWithFlag(ctx, f, o.repo, args)
	if err != nil {
		return err
	}
	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	if err := client.SyncFork(ctx, ref.Owner, ref.Name, o.branch); err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found or is not a fork", ref)
		}
		return err
	}
	what := ref.String()
	if o.branch != "" {
		what = fmt.Sprintf("%s (branch %s)", ref, o.branch)
	}
	reportName(f, "Synced fork", what)
	return nil
}

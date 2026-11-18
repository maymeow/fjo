// Package release implements Forgejo release commands.
package release

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/output"
	"github.com/maymeow/fjo/internal/reporef"
)

// NewCmd returns the release command group.
func NewCmd(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "release <command>", Short: "Work with Forgejo releases"}
	cmd.AddCommand(newListCmd(f), newViewCmd(f), newCreateCmd(f), newEditCmd(f), newDeleteCmd(f))
	return cmd
}

func repoFlag(cmd *cobra.Command, repo *string) {
	cmd.Flags().StringVarP(repo, "repo", "R", "", "Select a repository: [HOST/]OWNER/REPO")
}

func resolve(ctx context.Context, f *app.Factory, repo string) (*api.Client, reporef.Ref, error) {
	ref, err := f.ResolveRepo(ctx, repo)
	if err != nil {
		return nil, ref, err
	}
	client, _, err := f.ClientFor(ref.Host)
	return client, ref, err
}

func jsonOptions(f *app.Factory) output.WriteOptions {
	return output.WriteOptions{TTY: f.IO.IsStdoutTTY(), Color: f.IO.ColorEnabled(), Width: f.IO.TerminalWidth()}
}

func page(ctx context.Context, f *app.Factory, disabled bool, write func(io.Writer) error) error {
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	pager := output.ResolvePager(f.Getenv, cfg.Defaults.Pager)
	return pager.Run(ctx, f.IO.Out, f.IO.ErrOut, output.PagerOptions{Disabled: disabled}, write)
}

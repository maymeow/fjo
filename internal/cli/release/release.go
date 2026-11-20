// Package release implements Forgejo release commands.
package release

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/reporef"
)

// NewCmd returns the release command group.
func NewCmd(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "release <command>", Short: "Work with Forgejo releases"}
	cmd.AddCommand(newListCmd(f), newViewCmd(f), newCreateCmd(f), newEditCmd(f), newDeleteCmd(f),
		newUploadCmd(f), newDeleteAssetCmd(f), newDownloadCmd(f))
	return cmd
}

func resolve(ctx context.Context, f *app.Factory, repo string) (*api.Client, reporef.Ref, error) {
	ref, err := f.ResolveRepo(ctx, repo)
	if err != nil {
		return nil, ref, err
	}
	client, _, err := f.ClientFor(ref.Host)
	return client, ref, err
}

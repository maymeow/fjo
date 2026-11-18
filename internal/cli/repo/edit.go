package repo

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type editOptions struct {
	repo          string
	description   string
	homepage      string
	visibility    string
	defaultBranch string

	descriptionSet   bool
	homepageSet      bool
	visibilitySet    bool
	defaultBranchSet bool
}

func newEditCmd(f *app.Factory) *cobra.Command {
	var o editOptions
	cmd := &cobra.Command{
		Use:   "edit [<repository>]",
		Short: "Edit repository properties",
		Long: `Edit the description, homepage, visibility or default branch of a repository.

Only the flags you pass are changed.`,
		Example: `  fjo repo edit --description "New description"
  fjo repo edit my-org/project --homepage https://example.com
  fjo repo edit my-org/project --visibility private
  fjo repo edit my-org/project --default-branch develop`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.descriptionSet = cmd.Flags().Changed("description")
			o.homepageSet = cmd.Flags().Changed("homepage")
			o.visibilitySet = cmd.Flags().Changed("visibility")
			o.defaultBranchSet = cmd.Flags().Changed("default-branch")
			return runEdit(cmd.Context(), f, o, args)
		},
	}
	cmdutil.AddRepoFlag(cmd, &o.repo)
	fl := cmd.Flags()
	fl.StringVar(&o.description, "description", "", "Repository description")
	fl.StringVar(&o.homepage, "homepage", "", "Repository homepage URL")
	fl.StringVar(&o.visibility, "visibility", "", `Repository visibility: "public" or "private"`)
	fl.StringVar(&o.defaultBranch, "default-branch", "", "Default branch name")
	return cmd
}

func runEdit(ctx context.Context, f *app.Factory, o editOptions, args []string) error {
	ref, err := repoArgWithFlag(ctx, f, o.repo, args)
	if err != nil {
		return err
	}
	if !o.descriptionSet && !o.homepageSet && !o.visibilitySet && !o.defaultBranchSet {
		return fjoerr.Usage("nothing to change: pass --description, --homepage, --visibility or --default-branch")
	}
	var patch api.EditRepoOptions
	if o.descriptionSet {
		patch.Description = &o.description
	}
	if o.homepageSet {
		patch.Website = &o.homepage
	}
	if o.visibilitySet {
		switch o.visibility {
		case "public":
			v := false
			patch.Private = &v
		case "private":
			v := true
			patch.Private = &v
		default:
			return fjoerr.Usage(`invalid --visibility %q: use "public" or "private"`, o.visibility)
		}
	}
	if o.defaultBranchSet {
		if o.defaultBranch == "" {
			return fjoerr.Usage("--default-branch must not be empty")
		}
		patch.DefaultBranch = &o.defaultBranch
	}

	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	repo, err := client.EditRepository(ctx, ref.Owner, ref.Name, patch)
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found", ref).
				WithHint("check the name and that your account can edit it")
		}
		return err
	}
	reportRepo(f, "Updated repository", repo)
	return nil
}

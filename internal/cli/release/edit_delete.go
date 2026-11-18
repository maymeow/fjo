package release

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
)

func newEditCmd(f *app.Factory) *cobra.Command {
	var repo, tag, title, target string
	var draft, prerelease bool
	var notes notesOptions
	cmd := &cobra.Command{Use: "edit <tag>", Short: "Edit a release", Args: cobra.ExactArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	flags := cmd.Flags()
	flags.StringVar(&tag, "tag", "", "Change the release tag")
	flags.StringVarP(&title, "title", "t", "", "Change the title")
	flags.StringVar(&target, "target", "", "Change the target commit or branch")
	flags.BoolVarP(&draft, "draft", "d", false, "Set draft state (use --draft=false to publish)")
	flags.BoolVarP(&prerelease, "prerelease", "p", false, "Set pre-release state")
	notes.addFlags(flags)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := notes.validate(); err != nil {
			return err
		}
		payload := api.EditReleaseOptions{}
		if flags.Changed("tag") {
			if err := validateTag(tag); err != nil {
				return err
			}
			payload.TagName = &tag
		}
		if flags.Changed("title") {
			payload.Name = &title
		}
		if flags.Changed("target") {
			if strings.TrimSpace(target) == "" {
				return fjoerr.Usage("--target requires a commit or branch")
			}
			payload.TargetCommitish = &target
		}
		if flags.Changed("draft") {
			payload.Draft = &draft
		}
		if flags.Changed("prerelease") {
			payload.Prerelease = &prerelease
		}
		if payload == (api.EditReleaseOptions{}) && !notes.provided() {
			return fjoerr.Usage("specify at least one release field to edit")
		}
		return editRelease(cmd.Context(), f, repo, args[0], payload, notes)
	}
	return cmd
}

func editRelease(ctx context.Context, f *app.Factory, repo, tag string, payload api.EditReleaseOptions, notes notesOptions) error {
	if err := validateTag(tag); err != nil {
		return err
	}
	client, ref, err := resolve(ctx, f, repo)
	if err != nil {
		return err
	}
	current, err := client.ReleaseByTag(ctx, ref.Owner, ref.Name, tag)
	if err != nil {
		return err
	}
	if current.ID <= 0 {
		return fjoerr.New(fjoerr.KindGeneral, "Forgejo did not return a valid release ID")
	}
	if payload.Name != nil && *payload.Name == "" && current.Name != "" {
		return fjoerr.New(fjoerr.KindUnsupported, "Forgejo cannot clear an existing release title")
	}
	if payload.TagName != nil && *payload.TagName != current.TagName {
		if err := checkDuplicate(ctx, client, ref.Owner, ref.Name, *payload.TagName); err != nil {
			return err
		}
	}
	if notes.provided() {
		text, err := notes.read(ctx, f)
		if err != nil {
			return err
		}
		if text == "" && current.Body != "" {
			return fjoerr.New(fjoerr.KindUnsupported, "Forgejo cannot clear existing release notes")
		}
		payload.Body = &text
	}
	release, err := client.EditRelease(ctx, ref.Owner, ref.Name, current.ID, payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(f.IO.Out, cmdutil.OneLine(release.HTMLURL))
	return err
}

func newDeleteCmd(f *app.Factory) *cobra.Command {
	var repo string
	var cleanup, yes bool
	cmd := &cobra.Command{Use: "delete <tag>", Short: "Delete a release", Args: cobra.ExactArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm deletion without prompting")
	cmd.Flags().BoolVar(&cleanup, "cleanup-tag", false, "Also delete the Git tag from Forgejo")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		tag := args[0]
		if err := validateTag(tag); err != nil {
			return err
		}
		if !yes && !f.IO.CanPrompt() {
			return fjoerr.Usage("--yes is required without a terminal")
		}
		ctx := cmd.Context()
		client, ref, err := resolve(ctx, f, repo)
		if err != nil {
			return err
		}
		release, err := client.ReleaseByTag(ctx, ref.Owner, ref.Name, tag)
		if err != nil {
			return err
		}
		if release.ID <= 0 || release.TagName != tag {
			return fjoerr.New(fjoerr.KindGeneral, "Forgejo did not return an ID for the requested release tag")
		}
		if !yes {
			action := "delete the release"
			if cleanup {
				action = "delete the release and its remote Git tag"
			}
			reply, err := f.Prompter.Input(fmt.Sprintf("Type %s to %s in %s:", tag, action, ref.FullName()))
			if err != nil {
				return err
			}
			if reply != tag {
				return fjoerr.New(fjoerr.KindCancelled, "deletion cancelled")
			}
		}
		if err := client.DeleteRelease(ctx, ref.Owner, ref.Name, release.ID); err != nil {
			return err
		}
		if cleanup {
			err := client.DeleteTag(ctx, ref.Owner, ref.Name, tag)
			if err != nil && fjoerr.KindOf(err) != fjoerr.KindNotFound {
				return fjoerr.Wrap(fjoerr.KindOf(err), err, "release deleted, but removing Git tag %q failed", tag)
			}
		}
		fmt.Fprintf(f.IO.ErrOut, "Deleted release %s from %s\n", tag, ref.FullName())
		return nil
	}
	return cmd
}

package release

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

type notesOptions struct {
	text, file string
	flags      *pflag.FlagSet
}

func (o *notesOptions) addFlags(flags *pflag.FlagSet) {
	o.flags = flags
	flags.StringVarP(&o.text, "notes", "n", "", "Release notes")
	flags.StringVarP(&o.file, "notes-file", "F", "", "Read release notes from a file (use - for standard input)")
}

func (o notesOptions) provided() bool {
	return o.flags.Changed("notes") || o.flags.Changed("notes-file")
}

func (o notesOptions) validate() error {
	if !o.flags.Changed("notes-file") {
		return nil
	}
	if o.flags.Changed("notes") {
		return fjoerr.Usage("--notes and --notes-file cannot be combined")
	}
	if o.file == "" {
		return fjoerr.Usage("--notes-file requires a file name (use - for standard input)")
	}
	return nil
}

func (o notesOptions) read(ctx context.Context, f *app.Factory) (string, error) {
	opts := output.BodyOptions{Body: o.text, BodySet: o.flags.Changed("notes"), BodyFile: o.file}
	text, err := opts.Read(ctx, output.BodyContext{Stdin: f.IO.In})
	if err != nil {
		return "", fjoerr.Wrap(fjoerr.KindOf(err), err, "read release notes")
	}
	return text, nil
}

func validateTag(tag string) error {
	if tag == "" || strings.HasSuffix(tag, ".") || strings.Contains(tag, "..") || strings.Contains(tag, "@{") ||
		strings.ContainsAny(tag, " ~^:?*[\\") || strings.IndexFunc(tag, func(r rune) bool { return r < ' ' || r == 127 }) >= 0 {
		return fjoerr.Usage("invalid release tag %q", tag)
	}
	for part := range strings.SplitSeq(tag, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return fjoerr.Usage("invalid release tag %q", tag)
		}
	}
	return nil
}

type createOptions struct {
	repo                    string
	payload                 api.CreateReleaseOptions
	notes                   notesOptions
	verifyTag, notesFromTag bool
}

func newCreateCmd(f *app.Factory) *cobra.Command {
	var opts createOptions
	cmd := &cobra.Command{Use: "create [<tag>] [<files>...]", Short: "Create a release", Args: cobra.ArbitraryArgs}
	cmdutil.AddRepoFlag(cmd, &opts.repo)
	flags := cmd.Flags()
	flags.StringVarP(&opts.payload.Name, "title", "t", "", "Release title")
	flags.StringVar(&opts.payload.TargetCommitish, "target", "", "Commit or branch to tag (defaults to the repository default branch)")
	flags.BoolVarP(&opts.payload.Draft, "draft", "d", false, "Save as a draft")
	flags.BoolVarP(&opts.payload.Prerelease, "prerelease", "p", false, "Mark as a pre-release")
	flags.BoolVar(&opts.payload.HideArchiveLinks, "hide-archive-links", false, "Hide the source archive links")
	flags.BoolVar(&opts.verifyTag, "verify-tag", false, "Require the tag to exist on Forgejo")
	flags.BoolVar(&opts.notesFromTag, "notes-from-tag", false, "Read notes from the local annotated Git tag")
	opts.notes.addFlags(flags)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.notes.validate(); err != nil {
			return err
		}
		if opts.notesFromTag && opts.notes.provided() {
			return fjoerr.Usage("--notes-from-tag cannot be combined with --notes or --notes-file")
		}
		if flags.Changed("target") && strings.TrimSpace(opts.payload.TargetCommitish) == "" {
			return fjoerr.Usage("--target requires a commit or branch")
		}
		return createRelease(cmd.Context(), f, opts, args)
	}
	return cmd
}

func createRelease(ctx context.Context, f *app.Factory, opts createOptions, args []string) error {
	var err error
	payload := opts.payload
	var files []assetFile
	if len(args) > 0 {
		payload.TagName = args[0]
		if files, err = localAssets(args[1:]); err != nil {
			return err
		}
	} else {
		if !f.IO.CanPrompt() {
			return fjoerr.Usage("a tag is required without a terminal")
		}
		payload.TagName, err = f.Prompter.Input("Tag:")
		if err != nil {
			return err
		}
	}
	if err := validateTag(payload.TagName); err != nil {
		return err
	}
	client, ref, err := resolve(ctx, f, opts.repo)
	if err != nil {
		return err
	}
	if err := checkDuplicate(ctx, client, ref.Owner, ref.Name, payload.TagName); err != nil {
		return err
	}
	if len(files) > 0 {
		if err := requireAttachments(ctx, client); err != nil {
			return err
		}
	}
	if opts.verifyTag {
		err := client.VerifyTag(ctx, ref.Owner, ref.Name, payload.TagName)
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.Wrap(fjoerr.KindNotFound, err, "tag %q does not exist on Forgejo", payload.TagName)
		}
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(payload.Name) == "" {
		if f.IO.CanPrompt() {
			payload.Name, err = f.Prompter.Input(fmt.Sprintf("Title [%s]:", output.Sanitize(payload.TagName)))
			if err != nil {
				return err
			}
		}
		if strings.TrimSpace(payload.Name) == "" {
			payload.Name = payload.TagName
		}
	}
	if opts.notesFromTag {
		payload.Body, err = tagNotes(ctx, f, payload.TagName)
	} else {
		payload.Body, err = opts.notes.read(ctx, f)
	}
	if err != nil {
		return err
	}
	publish := !payload.Draft && len(files) > 0
	payload.Draft = payload.Draft || len(files) > 0
	release, err := client.CreateRelease(ctx, ref.Owner, ref.Name, payload)
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindConflict {
			if duplicate := checkDuplicate(ctx, client, ref.Owner, ref.Name, payload.TagName); fjoerr.KindOf(duplicate) == fjoerr.KindConflict {
				return duplicate
			}
		}
		return err
	}
	if len(files) > 0 {
		if release, err = uploadAndPublish(ctx, f, client, ref.Owner, ref.Name, release, files, publish); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(f.IO.Out, cmdutil.OneLine(release.HTMLURL))
	return err
}

// uploadAndPublish uploads the assets of a release created as a draft and
// publishes it afterwards, so nobody sees a published release without its
// assets. On failure the draft stays and the error says how to continue.
func uploadAndPublish(ctx context.Context, f *app.Factory, client *api.Client, owner, repo string, release *api.Release, files []assetFile, publish bool) (*api.Release, error) {
	tag := release.TagName
	if release.ID <= 0 {
		return nil, fjoerr.New(fjoerr.KindGeneral, "Forgejo did not return a valid release ID")
	}
	u := &uploader{f: f, client: client, owner: owner, repo: repo, release: release}
	if err := u.uploadAll(ctx, files); err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindOf(err), err, "draft release %q was created, but uploading its assets failed", tag).
			WithHint(fmt.Sprintf("upload the remaining files with 'fjo release upload %s', then publish with 'fjo release edit %s --draft=false'", tag, tag))
	}
	if !publish {
		return release, nil
	}
	draft := false
	published, err := client.EditRelease(ctx, owner, repo, release.ID, api.EditReleaseOptions{Draft: &draft})
	if err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindOf(err), err, "assets uploaded, but publishing draft release %q failed", tag).
			WithHint(fmt.Sprintf("publish it with 'fjo release edit %s --draft=false'", tag))
	}
	return published, nil
}

func checkDuplicate(ctx context.Context, c *api.Client, owner, repo, tag string) error {
	existing, err := c.ReleaseByTag(ctx, owner, repo, tag)
	switch fjoerr.KindOf(err) {
	case fjoerr.KindNotFound:
		return nil
	case "":
		if existing.HTMLURL == "" {
			return fjoerr.New(fjoerr.KindConflict, "release for tag %q already exists", tag)
		}
		return fjoerr.New(fjoerr.KindConflict, "release for tag %q already exists: %s", tag, cmdutil.OneLine(existing.HTMLURL))
	default:
		return err
	}
}

func tagNotes(ctx context.Context, f *app.Factory, tag string) (string, error) {
	ref := "refs/tags/" + tag
	out, err := f.Git().Run(ctx, "for-each-ref", "--count=1", "--format=%(refname)%00%(objecttype)%00%(contents:subject)%0a%0a%(contents:body)", "--", ref)
	if err != nil {
		return "", fjoerr.Wrap(fjoerr.KindOf(err), err, "read annotation for tag %q", tag)
	}
	fields := strings.SplitN(out, "\x00", 3)
	if len(fields) != 3 || fields[0] != ref {
		return "", fjoerr.New(fjoerr.KindNotFound, "local Git tag %q was not found", tag)
	}
	if fields[1] != "tag" {
		return "", fjoerr.Usage("tag %q is lightweight; --notes-from-tag requires an annotated tag", tag)
	}
	return strings.TrimSpace(fields[2]), nil
}

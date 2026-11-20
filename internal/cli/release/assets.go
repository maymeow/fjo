package release

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

// assetFile is a local file to upload and the asset name it gets.
type assetFile struct {
	path, name string
}

// localAssets validates the files before any request is sent: each must
// be a regular file and the asset names must be unique.
func localAssets(paths []string) ([]assetFile, error) {
	files := make([]assetFile, 0, len(paths))
	seen := map[string]string{}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fjoerr.Wrap(fjoerr.KindUsage, err, "cannot read asset file %s", path)
		}
		if !info.Mode().IsRegular() {
			return nil, fjoerr.Usage("asset %s is not a regular file", path)
		}
		name := filepath.Base(path)
		if other, ok := seen[name]; ok {
			return nil, fjoerr.Usage("%s and %s would both be uploaded as %q", other, path, name)
		}
		seen[name] = path
		files = append(files, assetFile{path: path, name: name})
	}
	return files, nil
}

func requireAttachments(ctx context.Context, client *api.Client) error {
	settings, err := client.AttachmentSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return fjoerr.New(fjoerr.KindUnsupported, "attachments are disabled on %s", client.Host())
	}
	return nil
}

func findAsset(release *api.Release, name string) *api.Attachment {
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i]
		}
	}
	return nil
}

// uploader uploads assets to one release, one file at a time.
type uploader struct {
	f           *app.Factory
	client      *api.Client
	owner, repo string
	release     *api.Release
	clobber     bool
}

func (u *uploader) checkExisting(files []assetFile) error {
	if u.clobber {
		return nil
	}
	for _, file := range files {
		if findAsset(u.release, file.name) != nil {
			return fjoerr.New(fjoerr.KindConflict, "asset %q already exists in release %q", file.name, u.release.TagName).
				WithHint("use --clobber to replace it")
		}
	}
	return nil
}

// uploadAll stops at the first failure and names the assets that were
// already uploaded, because Forgejo has no way to upload them atomically.
func (u *uploader) uploadAll(ctx context.Context, files []assetFile) error {
	for i, file := range files {
		if err := u.upload(ctx, file); err != nil {
			return partialUploadError(err, files[:i], files[i:])
		}
	}
	return nil
}

func partialUploadError(err error, done, pending []assetFile) error {
	if len(done) == 0 {
		return err
	}
	return fjoerr.Wrap(fjoerr.KindOf(err), err, "uploaded %s; not uploaded %s", assetNames(done), assetNames(pending))
}

func assetNames(files []assetFile) string {
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = file.name
	}
	return strings.Join(names, ", ")
}

// upload replaces an existing asset only after the new content is stored
// under a temporary name, so a failed transfer never loses the old asset.
func (u *uploader) upload(ctx context.Context, file assetFile) error {
	previous := findAsset(u.release, file.name)
	if previous == nil {
		_, err := u.send(ctx, file, file.name)
		return err
	}
	temporary, err := temporaryAssetName(file.name)
	if err != nil {
		return err
	}
	asset, err := u.send(ctx, file, temporary)
	if err != nil {
		return err
	}
	if err := u.client.DeleteReleaseAsset(ctx, u.owner, u.repo, u.release.ID, previous.ID); err != nil {
		return fjoerr.Wrap(fjoerr.KindOf(err), err, "uploaded %s as %q, but removing the previous asset failed", file.name, temporary)
	}
	if _, err := u.client.RenameReleaseAsset(ctx, u.owner, u.repo, u.release.ID, asset.ID, file.name); err != nil {
		return fjoerr.Wrap(fjoerr.KindOf(err), err, "replaced %s, but renaming the upload from %q failed", file.name, temporary).
			WithHint("rename the asset in the Forgejo web interface")
	}
	return nil
}

func (u *uploader) send(ctx context.Context, file assetFile, name string) (asset *api.Attachment, err error) {
	fh, err := os.Open(file.path)
	if err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindGeneral, err, "open %s", file.path)
	}
	defer func() { err = fjoerr.JoinCleanup(err, fh.Close()) }()
	info, err := fh.Stat()
	if err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindGeneral, err, "read %s", file.path)
	}
	var body io.Reader = fh
	progress := &progressReader{r: fh, w: u.f.IO.ErrOut, name: output.Sanitize(file.name), total: info.Size(), last: -1}
	if u.f.IO.IsStderrTTY() {
		body = progress
		progress.report()
	}
	asset, err = u.client.UploadReleaseAsset(ctx, u.owner, u.repo, u.release.ID, name, body, info.Size())
	if body == progress {
		progress.finish(err == nil)
	}
	if err != nil {
		return nil, uploadError(err, file.name)
	}
	return asset, nil
}

func uploadError(err error, name string) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusRequestEntityTooLarge {
		return fjoerr.Wrap(fjoerr.KindConflict, err, "Forgejo rejected %s: it exceeds the server's size limit or storage quota", name)
	}
	return fjoerr.Wrap(fjoerr.KindOf(err), err, "upload %s", name)
}

func temporaryAssetName(name string) (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "generate a temporary asset name")
	}
	return "fjo-upload-" + hex.EncodeToString(random) + "-" + name, nil
}

// progressReader reports upload progress on a terminal whenever the
// completed percentage changes.
type progressReader struct {
	r           io.Reader
	w           io.Writer
	name        string
	done, total int64
	last        int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.report()
	return n, err
}

func (p *progressReader) percent() int64 {
	if p.total <= 0 {
		return 100
	}
	return p.done * 100 / p.total
}

func (p *progressReader) report() {
	if percent := p.percent(); percent != p.last {
		p.last = percent
		fmt.Fprintf(p.w, "\rUploading %s %3d%% (%s / %s)", p.name, percent, formatBytes(p.done), formatBytes(p.total))
	}
}

func (p *progressReader) finish(ok bool) {
	if ok {
		fmt.Fprintf(p.w, "\rUploaded %s (%s)%s\n", p.name, formatBytes(p.total), strings.Repeat(" ", 24))
		return
	}
	fmt.Fprintln(p.w)
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func newUploadCmd(f *app.Factory) *cobra.Command {
	var repo string
	var clobber bool
	cmd := &cobra.Command{Use: "upload <tag> <files>...", Short: "Upload assets to a release", Args: cobra.MinimumNArgs(2)}
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.Flags().BoolVar(&clobber, "clobber", false, "Replace existing assets with the same name")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		tag := args[0]
		if err := validateTag(tag); err != nil {
			return err
		}
		files, err := localAssets(args[1:])
		if err != nil {
			return err
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
		if release.ID <= 0 {
			return fjoerr.New(fjoerr.KindGeneral, "Forgejo did not return a valid release ID")
		}
		u := &uploader{f: f, client: client, owner: ref.Owner, repo: ref.Name, release: release, clobber: clobber}
		if err := u.checkExisting(files); err != nil {
			return err
		}
		if err := requireAttachments(ctx, client); err != nil {
			return err
		}
		return u.uploadAll(ctx, files)
	}
	return cmd
}

func newDeleteAssetCmd(f *app.Factory) *cobra.Command {
	var repo string
	var yes bool
	cmd := &cobra.Command{Use: "delete-asset <tag> <asset-name>", Short: "Delete an asset from a release", Args: cobra.ExactArgs(2)}
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm deletion without prompting")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		tag, name := args[0], args[1]
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
		asset := findAsset(release, name)
		if asset == nil {
			return fjoerr.New(fjoerr.KindNotFound, "release %q has no asset named %q", tag, name)
		}
		if !yes {
			prompt := fmt.Sprintf("Type %s to delete the asset from release %s in %s:", output.Sanitize(name), tag, ref.FullName())
			if err := confirmByTyping(f, name, prompt); err != nil {
				return err
			}
		}
		if err := client.DeleteReleaseAsset(ctx, ref.Owner, ref.Name, release.ID, asset.ID); err != nil {
			return err
		}
		fmt.Fprintf(f.IO.ErrOut, "Deleted asset %s from release %s\n", output.Sanitize(name), tag)
		return nil
	}
	return cmd
}

// confirmByTyping asks the user to type expected before a destructive action.
func confirmByTyping(f *app.Factory, expected, prompt string) error {
	reply, err := f.Prompter.Input(prompt)
	if err != nil {
		return err
	}
	if reply != expected {
		return fjoerr.New(fjoerr.KindCancelled, "deletion cancelled")
	}
	return nil
}

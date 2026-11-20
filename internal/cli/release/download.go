package release

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
	"github.com/maymeow/fjo/internal/reporef"
)

// maxManifestSize bounds how much of a checksum manifest is read into memory.
const maxManifestSize = 1 << 20

type downloadOptions struct {
	repo                              string
	patterns                          []string
	archive, dir, output              string
	clobber, skipExisting, skipVerify bool
}

func (o downloadOptions) validate(dirChanged bool) error {
	switch {
	case o.clobber && o.skipExisting:
		return fjoerr.Usage("--clobber and --skip-existing cannot be combined")
	case o.archive != "" && o.archive != "zip" && o.archive != "tar.gz":
		return fjoerr.Usage("--archive must be zip or tar.gz")
	case o.archive != "" && len(o.patterns) > 0:
		return fjoerr.Usage("--archive and --pattern cannot be combined")
	case o.output != "" && dirChanged:
		return fjoerr.Usage("--output and --dir cannot be combined")
	case o.dir == "":
		return fjoerr.Usage("--dir requires a directory")
	}
	for _, pattern := range o.patterns {
		if _, err := path.Match(pattern, ""); err != nil {
			return fjoerr.Usage("invalid --pattern %q: %v", pattern, err)
		}
	}
	return nil
}

// downloadTarget is one file to download. size is -1 when unknown.
type downloadTarget struct {
	name, url string
	size      int64
}

func newDownloadCmd(f *app.Factory) *cobra.Command {
	var opts downloadOptions
	cmd := &cobra.Command{Use: "download [<tag>]", Short: "Download release assets", Args: cobra.MaximumNArgs(1)}
	cmdutil.AddRepoFlag(cmd, &opts.repo)
	flags := cmd.Flags()
	flags.StringArrayVarP(&opts.patterns, "pattern", "p", nil, "Download only assets that match a glob pattern (repeatable)")
	flags.StringVarP(&opts.archive, "archive", "A", "", "Download the source archive: zip or tar.gz")
	flags.StringVarP(&opts.dir, "dir", "D", ".", "Directory to download into")
	flags.StringVarP(&opts.output, "output", "O", "", "Write the single downloaded file to this path (use - for standard output)")
	flags.BoolVar(&opts.clobber, "clobber", false, "Overwrite existing files")
	flags.BoolVar(&opts.skipExisting, "skip-existing", false, "Skip files that already exist")
	flags.BoolVar(&opts.skipVerify, "skip-verify", false, "Do not verify SHA-256 checksums from a checksum manifest")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.validate(flags.Changed("dir")); err != nil {
			return err
		}
		if len(args) > 0 {
			if err := validateTag(args[0]); err != nil {
				return err
			}
		}
		ctx := cmd.Context()
		client, ref, err := resolve(ctx, f, opts.repo)
		if err != nil {
			return err
		}
		release, err := findRelease(ctx, client, ref, args)
		if err != nil {
			return err
		}
		targets, err := selectTargets(release, ref, opts)
		if err != nil {
			return err
		}
		if opts.output != "" && len(targets) > 1 {
			return fjoerr.Usage("--output needs exactly one file, but %d assets match", len(targets))
		}
		d := &downloader{f: f, client: client, opts: opts}
		if !opts.skipVerify && opts.archive == "" {
			if d.checksums, err = loadChecksums(ctx, client, release); err != nil {
				return err
			}
		}
		for _, target := range targets {
			if err := d.download(ctx, target); err != nil {
				return err
			}
		}
		return nil
	}
	return cmd
}

func selectTargets(release *api.Release, ref reporef.Ref, opts downloadOptions) ([]downloadTarget, error) {
	if opts.archive != "" {
		link := release.TarballURL
		if opts.archive == "zip" {
			link = release.ZipballURL
		}
		if link == "" {
			return nil, fjoerr.New(fjoerr.KindUnsupported, "Forgejo did not return a %s archive link for release %q", opts.archive, release.TagName)
		}
		name := ref.Name + "-" + strings.ReplaceAll(release.TagName, "/", "-") + "." + opts.archive
		return []downloadTarget{{name: name, url: link, size: -1}}, nil
	}
	if len(release.Assets) == 0 {
		return nil, fjoerr.New(fjoerr.KindNotFound, "release %q has no assets", release.TagName)
	}
	var targets []downloadTarget
	for _, asset := range release.Assets {
		if !matchesAny(opts.patterns, asset.Name) {
			continue
		}
		size := asset.Size
		if asset.Type == "external" {
			size = -1
		}
		targets = append(targets, downloadTarget{name: asset.Name, url: asset.DownloadURL, size: size})
	}
	if len(targets) == 0 {
		return nil, fjoerr.New(fjoerr.KindNotFound, "no assets in release %q match the pattern", release.TagName)
	}
	return targets, nil
}

func matchesAny(patterns []string, name string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if ok, err := path.Match(pattern, name); err == nil && ok {
			return true
		}
	}
	return false
}

// isChecksumManifest recognises checksums.txt, GoReleaser's
// <project>_<version>_checksums.txt and SHA256SUMS (B-53).
func isChecksumManifest(name string) bool {
	lower := strings.ToLower(name)
	return lower == "checksums.txt" || lower == "sha256sums" || lower == "sha256sums.txt" || strings.HasSuffix(lower, "_checksums.txt")
}

// loadChecksums reads every checksum manifest of the release into a map
// from asset name to SHA-256 digest.
func loadChecksums(ctx context.Context, client *api.Client, release *api.Release) (map[string][]byte, error) {
	sums := map[string][]byte{}
	for _, asset := range release.Assets {
		if !isChecksumManifest(asset.Name) {
			continue
		}
		data, err := readManifest(ctx, client, asset)
		if err != nil {
			return nil, err
		}
		if err := parseChecksums(data, sums); err != nil {
			return nil, fjoerr.Wrap(fjoerr.KindGeneral, err, "checksum manifest %s", output.Sanitize(asset.Name))
		}
	}
	return sums, nil
}

func readManifest(ctx context.Context, client *api.Client, asset api.Attachment) (data []byte, err error) {
	resp, err := client.Download(ctx, asset.DownloadURL)
	if err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindOf(err), err, "download checksum manifest %s", output.Sanitize(asset.Name))
	}
	defer func() { err = fjoerr.JoinCleanup(err, resp.Body.Close()) }()
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxManifestSize+1))
	if err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindUnavailable, err, "download checksum manifest %s", output.Sanitize(asset.Name))
	}
	if len(data) > maxManifestSize {
		return nil, fjoerr.New(fjoerr.KindGeneral, "checksum manifest %s is larger than 1 MiB", output.Sanitize(asset.Name))
	}
	return data, nil
}

// parseChecksums adds SHA-256 entries in GNU ("<hex>  <name>", "<hex> *<name>")
// and BSD ("SHA256 (<name>) = <hex>") format to sums. Other lines, such as
// digests of other algorithms, are ignored.
func parseChecksums(data []byte, sums map[string][]byte) error {
	for line := range strings.Lines(string(data)) {
		name, digest, ok := parseChecksumLine(strings.TrimRight(line, "\r\n"))
		if !ok {
			continue
		}
		if previous, seen := sums[name]; seen && !bytes.Equal(previous, digest) {
			return fmt.Errorf("conflicting SHA-256 digests for %q", name)
		}
		sums[name] = digest
	}
	return nil
}

func parseChecksumLine(line string) (string, []byte, bool) {
	var name, digest string
	if rest, ok := strings.CutPrefix(line, "SHA256 ("); ok {
		var found bool
		name, digest, found = strings.Cut(rest, ") = ")
		if !found {
			return "", nil, false
		}
	} else {
		var found bool
		digest, name, found = strings.Cut(line, " ")
		if !found {
			return "", nil, false
		}
		name = strings.TrimPrefix(strings.TrimPrefix(name, " "), "*")
	}
	sum, err := hex.DecodeString(strings.TrimSpace(digest))
	if err != nil || len(sum) != sha256.Size || name == "" {
		return "", nil, false
	}
	return name, sum, true
}

// safeAssetName rejects server-supplied names that are not a plain file
// name. On Windows it also rejects reserved device names and characters.
func safeAssetName(name string, windows bool) error {
	bad := name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) ||
		strings.ContainsFunc(name, unicode.IsControl) || filepath.IsAbs(name)
	if windows && !bad {
		stem, _, _ := strings.Cut(name, ".")
		bad = strings.ContainsAny(name, `<>:"|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") ||
			isReservedWindowsName(strings.TrimRight(stem, " "))
	}
	if bad {
		return fjoerr.New(fjoerr.KindGeneral, "refusing to download asset with unsafe name %q", name)
	}
	return nil
}

func isReservedWindowsName(stem string) bool {
	upper := strings.ToUpper(stem)
	switch upper {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) {
		return upper[3] >= '0' && upper[3] <= '9'
	}
	return false
}

// downloader writes release files to disk or standard output.
type downloader struct {
	f         *app.Factory
	client    *api.Client
	opts      downloadOptions
	checksums map[string][]byte
}

func (d *downloader) download(ctx context.Context, target downloadTarget) error {
	switch d.opts.output {
	case "-":
		return d.toStdout(ctx, target)
	case "":
		if err := safeAssetName(target.name, runtime.GOOS == "windows"); err != nil {
			return err
		}
		return d.toFile(ctx, target, d.opts.dir, target.name)
	default:
		return d.toFile(ctx, target, filepath.Dir(d.opts.output), filepath.Base(d.opts.output))
	}
}

func (d *downloader) toStdout(ctx context.Context, target downloadTarget) error {
	result, err := d.fetch(ctx, target, d.f.IO.Out)
	if err != nil {
		return err
	}
	return d.verify(target, result)
}

// toFile streams into a temporary file in dir and renames it only after
// the size and checksum match, so no partial or corrupt file ever has the
// final name. os.Root keeps every path inside dir, even through symlinks.
func (d *downloader) toFile(ctx context.Context, target downloadTarget, dir, name string) (err error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "create directory %s", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "open directory %s", dir)
	}
	defer func() { err = fjoerr.JoinCleanup(err, root.Close()) }()
	exists, err := d.checkExisting(root, name)
	if err != nil || exists {
		return err
	}
	temporary, err := temporaryDownloadName()
	if err != nil {
		return err
	}
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "create a temporary file in %s", dir)
	}
	result, err := d.fetch(ctx, target, file)
	if err == nil {
		err = d.verify(target, result)
	}
	if err == nil {
		err = wrapWriteErr(file.Sync(), name)
	}
	err = errors.Join(err, wrapWriteErr(file.Close(), name))
	if err == nil {
		err = d.place(root, temporary, name)
	}
	if err != nil {
		return fjoerr.JoinCleanup(err, root.Remove(temporary))
	}
	return nil
}

// checkExisting applies --clobber and --skip-existing (B-54); it reports
// true when the file exists and should be skipped.
func (d *downloader) checkExisting(root *os.Root, name string) (bool, error) {
	_, err := root.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fjoerr.Wrap(fjoerr.KindGeneral, err, "check %s", name)
	case d.opts.skipExisting:
		if d.f.IO.IsStderrTTY() {
			fmt.Fprintf(d.f.IO.ErrOut, "Skipped %s: file exists\n", output.Sanitize(name))
		}
		return true, nil
	case d.opts.clobber:
		return false, nil
	default:
		return false, fjoerr.New(fjoerr.KindConflict, "%s already exists", output.Sanitize(name)).
			WithHint("use --clobber to overwrite it or --skip-existing to keep it")
	}
}

// place renames the finished download to its final name. Without
// --clobber it checks again, because the file may have appeared during
// the transfer.
func (d *downloader) place(root *os.Root, temporary, name string) error {
	if !d.opts.clobber {
		if _, err := d.checkExisting(root, name); err != nil {
			return err
		}
	}
	if err := root.Rename(temporary, name); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "save %s", name)
	}
	return nil
}

func temporaryDownloadName() (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "generate a temporary file name")
	}
	return ".fjo-download-" + hex.EncodeToString(random) + ".part", nil
}

type fetchResult struct {
	size   int64
	digest []byte
}

// errWriter remembers write errors so they can be told apart from
// network errors during the copy.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(b []byte) (int, error) {
	n, err := e.w.Write(b)
	if err != nil {
		e.err = err
	}
	return n, err
}

func wrapWriteErr(err error, name string) error {
	if err == nil {
		return nil
	}
	return fjoerr.Wrap(fjoerr.KindGeneral, err, "write %s", name)
}

func (d *downloader) fetch(ctx context.Context, target downloadTarget, w io.Writer) (result fetchResult, err error) {
	resp, err := d.client.Download(ctx, target.url)
	if err != nil {
		return result, fjoerr.Wrap(fjoerr.KindOf(err), err, "download %s", output.Sanitize(target.name))
	}
	defer func() { err = fjoerr.JoinCleanup(err, resp.Body.Close()) }()
	total := target.size
	if total < 0 {
		total = resp.ContentLength
	}
	hash := sha256.New()
	dst := &errWriter{w: w}
	progress := newProgress(d.f, resp.Body, "Downloading", "Downloaded", target.name, total)
	result.size, err = io.Copy(io.MultiWriter(dst, hash), progress.reader())
	progress.finish(err == nil, d.verifiedNote(target, hash.Sum(nil)))
	switch {
	case dst.err != nil:
		return result, wrapWriteErr(dst.err, target.name)
	case ctx.Err() != nil:
		return result, ctx.Err()
	case err != nil:
		return result, fjoerr.Wrap(fjoerr.KindUnavailable, err, "download %s", output.Sanitize(target.name))
	}
	result.digest = hash.Sum(nil)
	return result, nil
}

func (d *downloader) verifiedNote(target downloadTarget, digest []byte) string {
	if want, ok := d.checksums[target.name]; ok && bytes.Equal(want, digest) {
		return ", SHA-256 verified"
	}
	return ""
}

// verify compares the byte count with the size Forgejo reported and the
// digest with the checksum manifest, when the release has one (B-53).
func (d *downloader) verify(target downloadTarget, result fetchResult) error {
	name := output.Sanitize(target.name)
	if target.size >= 0 && result.size != target.size {
		return fjoerr.New(fjoerr.KindGeneral, "download of %s is incomplete: got %d of %d bytes", name, result.size, target.size)
	}
	if want, ok := d.checksums[target.name]; ok && !bytes.Equal(want, result.digest) {
		return fjoerr.New(fjoerr.KindGeneral, "SHA-256 checksum of %s does not match the checksum manifest", name).
			WithHint("use --skip-verify to skip the check")
	}
	return nil
}

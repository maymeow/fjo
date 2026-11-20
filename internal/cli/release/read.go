package release

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
	"github.com/maymeow/fjo/internal/reporef"
)

func newListCmd(f *app.Factory) *cobra.Command {
	var repo, order string
	var limit int
	var noPager bool
	var options api.ReleaseListOptions
	cmd := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List releases in a repository", Args: cobra.NoArgs}
	cmdutil.AddRepoFlag(cmd, &repo)
	flags := cmd.Flags()
	flags.IntVarP(&limit, "limit", "L", 30, "Maximum number of releases to list")
	flags.StringVarP(&order, "order", "O", "desc", "Order of releases by creation time: asc or desc")
	flags.BoolVar(&options.ExcludeDrafts, "exclude-drafts", false, "Exclude draft releases")
	flags.BoolVar(&options.ExcludePrereleases, "exclude-pre-releases", false, "Exclude pre-releases")
	flags.BoolVar(&noPager, "no-pager", false, "Disable the pager")
	jsonFlags := output.AddJSONFlags(cmd, output.ReleaseFields)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if limit < 1 {
			return fjoerr.Usage("--limit must be at least 1")
		}
		if order != "asc" && order != "desc" {
			return fjoerr.Usage("--order must be asc or desc")
		}
		ctx := cmd.Context()
		client, ref, err := resolve(ctx, f, repo)
		if err != nil {
			return err
		}
		items, err := client.ListReleases(ctx, ref.Owner, ref.Name, options, serverLimit(order, limit))
		if err != nil {
			return err
		}
		releases := make([]output.Release, 0, len(items))
		for _, item := range items {
			releases = append(releases, output.NewRelease(item))
		}
		sortReleases(releases, order)
		releases = releases[:min(limit, len(releases))]
		if jsonFlags.Changed() {
			return jsonFlags.Write(ctx, f.IO.Out, releases, cmdutil.JSONOptions(f))
		}
		if len(releases) == 0 {
			_, err := fmt.Fprintf(f.IO.ErrOut, "no releases found in %s\n", ref.FullName())
			return err
		}
		var latest *api.Release
		if slices.ContainsFunc(releases, func(r output.Release) bool { return !r.IsDraft && !r.IsPrerelease }) {
			latest, err = client.LatestRelease(ctx, ref.Owner, ref.Name)
			if err != nil && fjoerr.KindOf(err) != fjoerr.KindNotFound {
				return err
			}
		}
		table := releaseTable(releases, latest)
		return cmdutil.Page(ctx, f, noPager, func(w io.Writer) error {
			return table.Print(w, cmdutil.TableOptions(f))
		})
	}
	return cmd
}

// serverLimit returns how many releases to fetch. Forgejo lists releases
// newest first by creation time, so descending order needs only the first
// limit entries; ascending order needs all of them.
func serverLimit(order string, limit int) int {
	if order == "desc" {
		return limit
	}
	return 0
}

func sortReleases(items []output.Release, order string) {
	slices.SortStableFunc(items, func(a, b output.Release) int {
		result := a.CreatedAt.Compare(b.CreatedAt)
		if result == 0 {
			result = cmp.Compare(a.ID, b.ID)
		}
		if order == "desc" {
			return -result
		}
		return result
	})
}

func releaseTable(releases []output.Release, latest *api.Release) output.Table {
	table := output.Table{Headers: []string{"TITLE", "TYPE", "TAG", "PUBLISHED"}}
	for _, r := range releases {
		title := r.Name
		if title == "" {
			title = r.TagName
		}
		published := ""
		if r.PublishedAt != nil {
			published = r.PublishedAt.UTC().Format(time.RFC3339)
		}
		var kind string
		switch {
		case r.IsDraft:
			kind = "Draft"
		case r.IsPrerelease:
			kind = "Pre-release"
		case latest != nil && latest.TagName == r.TagName:
			kind = "Latest"
		}
		table.Rows = append(table.Rows, []output.Cell{
			output.Text(cmdutil.OneLine(title)), output.Text(kind), output.Text(cmdutil.OneLine(r.TagName)), output.Text(published),
		})
	}
	return table
}

func newViewCmd(f *app.Factory) *cobra.Command {
	var repo string
	var web, noPager bool
	cmd := &cobra.Command{Use: "view [<tag>]", Short: "View a release", Args: cobra.MaximumNArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the release in a browser")
	cmd.Flags().BoolVar(&noPager, "no-pager", false, "Disable the pager")
	jsonFlags := output.AddJSONFlags(cmd, output.ReleaseFields)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if web && jsonFlags.Changed() {
			return fjoerr.Usage("--web and --json cannot be combined")
		}
		ctx := cmd.Context()
		client, ref, err := resolve(ctx, f, repo)
		if err != nil {
			return err
		}
		item, err := findRelease(ctx, client, ref, args)
		if err != nil {
			return err
		}
		r := output.NewRelease(*item)
		if web {
			return openReleaseURL(f, r.URL)
		}
		if jsonFlags.Changed() {
			return jsonFlags.Write(ctx, f.IO.Out, r, cmdutil.JSONOptions(f))
		}
		return cmdutil.Page(ctx, f, noPager, func(w io.Writer) error {
			return printRelease(w, r)
		})
	}
	return cmd
}

// findRelease returns the release for the tag in args, or the latest stable
// published release when no tag is given.
func findRelease(ctx context.Context, client *api.Client, ref reporef.Ref, args []string) (*api.Release, error) {
	if len(args) > 0 {
		return client.ReleaseByTag(ctx, ref.Owner, ref.Name, args[0])
	}
	release, err := client.LatestRelease(ctx, ref.Owner, ref.Name)
	if fjoerr.KindOf(err) == fjoerr.KindNotFound {
		return nil, fjoerr.Wrap(fjoerr.KindNotFound, err, "no published stable release found in %s", ref.FullName())
	}
	return release, err
}

func printRelease(w io.Writer, r output.Release) error {
	title := r.Name
	if title == "" {
		title = r.TagName
	}
	var text strings.Builder
	fmt.Fprintf(&text, "%s\nTag: %s\n", cmdutil.OneLine(title), cmdutil.OneLine(r.TagName))
	if r.IsDraft {
		text.WriteString("Draft\n")
	} else if r.IsPrerelease {
		text.WriteString("Pre-release\n")
	}
	if r.PublishedAt != nil {
		fmt.Fprintf(&text, "Published: %s\n", r.PublishedAt.UTC().Format(time.RFC3339))
	}
	if r.Body != "" {
		fmt.Fprintf(&text, "\n%s\n", output.Sanitize(r.Body))
	}
	if len(r.Assets) > 0 {
		text.WriteString("\nAssets\n")
		for _, a := range r.Assets {
			fmt.Fprintf(&text, "  %s (%d bytes)\n", cmdutil.OneLine(a.Name), a.Size)
		}
	}
	if r.URL != "" {
		fmt.Fprintf(&text, "\n%s\n", cmdutil.OneLine(r.URL))
	}
	_, err := io.WriteString(w, text.String())
	return err
}

// openReleaseURL opens the html_url Forgejo returned, refusing anything that
// is not a plain http(s) URL because the value comes from the server.
func openReleaseURL(f *app.Factory, link string) error {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return fjoerr.New(fjoerr.KindGeneral, "Forgejo did not return a valid browser URL for the release")
	}
	return cmdutil.OpenBrowser(f, u.String())
}

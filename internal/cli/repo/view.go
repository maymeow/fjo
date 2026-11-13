package repo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type viewOptions struct {
	repo   string
	web    bool
	branch string
}

func newViewCmd(f *app.Factory) *cobra.Command {
	var o viewOptions
	cmd := &cobra.Command{
		Use:   "view [<repository>]",
		Short: "View a repository",
		Long: `Display the description and basic information of a Forgejo repository.

Without an argument the repository of the current directory is shown.`,
		Example: `  fjo repo view
  fjo repo view my-org/my-project
  fjo repo view git.example.com/my-org/my-project --web`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runView(cmd.Context(), f, o, args)
		},
	}
	cmd.Flags().BoolVarP(&o.web, "web", "w", false, "Open the repository in the browser")
	cmd.Flags().StringVarP(&o.branch, "branch", "b", "", "Show the given branch (with --web)")
	addRepoFlag(cmd, &o.repo)
	return cmd
}

func runView(ctx context.Context, f *app.Factory, o viewOptions, args []string) error {
	ref, err := repoArgWithFlag(ctx, f, o.repo, args)
	if err != nil {
		return err
	}
	if o.branch != "" && !o.web {
		return fjoerr.Usage("--branch requires --web")
	}
	if o.web {
		path := []string{}
		if o.branch != "" {
			path = append(path, "src", "branch", url.PathEscape(o.branch))
		}
		return openInBrowser(f, webURL(f, ref, path...))
	}

	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	repo, err := client.GetRepository(ctx, ref.Owner, ref.Name)
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found", ref).
				WithHint("check the name and that your account can access it")
		}
		return err
	}
	if f.IO.IsStdoutTTY() {
		printRepoTTY(f.IO.Out, repo)
	} else {
		printRepoRaw(f.IO.Out, repo)
	}
	return nil
}

func openInBrowser(f *app.Factory, u string) error {
	if f.IO.IsStderrTTY() {
		fmt.Fprintf(f.IO.ErrOut, "Opening %s in your browser.\n", u)
	}
	err := f.Browser(u)
	if errors.Is(err, browser.ErrNoBrowser) {
		fmt.Fprintln(f.IO.Out, u)
		return nil
	}
	if err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot open the browser").
			WithHint("open " + u + " manually or set FJO_BROWSER")
	}
	return nil
}

func visibility(r *api.Repository) string {
	switch {
	case r.Private:
		return "private"
	case r.Internal:
		return "internal"
	}
	return "public"
}

func printRepoTTY(w io.Writer, r *api.Repository) {
	fmt.Fprintln(w, r.FullName)
	if r.Description != "" {
		fmt.Fprintln(w, r.Description)
	} else {
		fmt.Fprintln(w, "No description provided")
	}
	fmt.Fprintln(w)

	tags := []string{capitalize(visibility(r))}
	if r.Fork && r.Parent != nil {
		tags = append(tags, "Fork of "+r.Parent.FullName)
	} else if r.Fork {
		tags = append(tags, "Fork")
	}
	if r.Mirror {
		tags = append(tags, "Mirror")
	}
	if r.Template {
		tags = append(tags, "Template")
	}
	if r.Archived {
		tags = append(tags, "Archived")
	}
	if r.Empty {
		tags = append(tags, "Empty")
	}
	fmt.Fprintf(w, "  %s\n", strings.Join(tags, " · "))
	if r.DefaultBranch != "" {
		fmt.Fprintf(w, "  Default branch: %s\n", r.DefaultBranch)
	}
	if r.Language != "" {
		fmt.Fprintf(w, "  Language: %s\n", r.Language)
	}
	if len(r.Topics) > 0 {
		fmt.Fprintf(w, "  Topics: %s\n", strings.Join(r.Topics, ", "))
	}
	fmt.Fprintf(w, "  %d stars · %d forks · %d open issues · %d open pull requests\n",
		r.Stars, r.Forks, r.OpenIssues, r.OpenPulls)
	if r.Website != "" {
		fmt.Fprintf(w, "  Website: %s\n", r.Website)
	}
	if !r.UpdatedAt.IsZero() {
		fmt.Fprintf(w, "  Updated: %s\n", r.UpdatedAt.Format("2006-01-02"))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "View this repository on Forgejo: %s\n", r.HTMLURL)
}

// printRepoRaw prints stable "key:\tvalue" lines for scripts (gh style).
func printRepoRaw(w io.Writer, r *api.Repository) {
	parent := ""
	if r.Parent != nil {
		parent = r.Parent.FullName
	}
	for _, kv := range [][2]string{
		{"name", r.FullName},
		{"description", r.Description},
		{"visibility", visibility(r)},
		{"default_branch", r.DefaultBranch},
		{"fork", fmt.Sprint(r.Fork)},
		{"parent", parent},
		{"mirror", fmt.Sprint(r.Mirror)},
		{"archived", fmt.Sprint(r.Archived)},
		{"stars", fmt.Sprint(r.Stars)},
		{"forks", fmt.Sprint(r.Forks)},
		{"open_issues", fmt.Sprint(r.OpenIssues)},
		{"open_pull_requests", fmt.Sprint(r.OpenPulls)},
		{"url", r.HTMLURL},
		{"clone_url", r.CloneURL},
		{"ssh_url", r.SSHURL},
	} {
		fmt.Fprintf(w, "%s:\t%s\n", kv[0], kv[1])
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

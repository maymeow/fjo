// Package issue implements Forgejo issue commands.
package issue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
	"github.com/maymeow/fjo/internal/reporef"
	"github.com/spf13/cobra"
)

func NewCmd(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "issue <command>", Short: "Work with Forgejo issues"}
	cmd.AddCommand(newListCmd(f), newViewCmd(f), newCreateCmd(f), newEditCmd(f))
	for _, action := range []string{"close", "reopen", "comment", "delete", "pin", "unpin"} {
		cmd.AddCommand(newActionCmd(f, action))
	}
	return cmd
}

func repoFlag(cmd *cobra.Command, p *string) {
	cmd.Flags().StringVarP(p, "repo", "R", "", "Select a repository: [HOST/]OWNER/REPO")
}

// An issue URL selects both the instance and repository, including subpaths.
func resolve(ctx context.Context, f *app.Factory, repo, arg string) (reporef.Ref, int, error) {
	if strings.Contains(arg, "://") {
		u, err := url.Parse(arg)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" {
			return reporef.Ref{}, 0, fjoerr.Usage("invalid issue URL %q", arg)
		}
		parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
		if len(parts) < 5 || parts[len(parts)-2] != "issues" {
			return reporef.Ref{}, 0, fjoerr.Usage("expected an issue URL")
		}
		number, err := parseNumber(parts[len(parts)-1])
		if err != nil {
			return reporef.Ref{}, 0, err
		}
		u.Path = strings.Join(parts[:len(parts)-2], "/")
		u.RawPath = ""
		u.Fragment = ""
		// Unlike a clone URL, an issue URL identifies owner/repo unambiguously
		// by the two segments before /issues/N. The preceding path is the
		// instance base path, including for a host not yet in config.yaml.
		ref, err := f.ParseRepo(u.Host + u.Path)
		return ref, number, err
	}
	number, err := parseNumber(arg)
	if err != nil {
		return reporef.Ref{}, 0, err
	}
	ref, err := f.ResolveRepo(ctx, repo)
	return ref, number, err
}
func parseNumber(s string) (int, error) {
	s = strings.TrimPrefix(s, "#")
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, fjoerr.Usage("invalid issue number %q", s)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fjoerr.Usage("invalid issue number %q", s)
	}
	return n, nil
}
func getIssue(ctx context.Context, c *api.Client, ref reporef.Ref, n int) (*api.Issue, error) {
	v, err := c.GetIssue(ctx, ref.Owner, ref.Name, n)
	if err != nil {
		return nil, err
	}
	if v.PullRequest != nil {
		return nil, fjoerr.Usage("#%d is a pull request; use 'fjo pr'", n)
	}
	return v, nil
}
func webURL(f *app.Factory, ref reporef.Ref, path string) string {
	base := "https://" + ref.Host
	if cfg, err := f.Config(); err == nil {
		if h := cfg.Host(ref.Host); h != nil {
			if h.InsecureHTTP {
				base = "http://" + ref.Host
			}
			if v, ok := strings.CutSuffix(strings.TrimRight(h.APIURL, "/"), "/api/v1"); ok {
				base = v
			}
		}
	}
	return base + "/" + url.PathEscape(ref.Owner) + "/" + url.PathEscape(ref.Name) + "/" + path
}
func openBrowser(f *app.Factory, u string) error {
	err := f.Browser(u)
	if errors.Is(err, browser.ErrNoBrowser) {
		_, err = fmt.Fprintln(f.IO.Out, u)
		return err
	}
	if err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot open the browser").WithHint("open " + u + " manually or set FJO_BROWSER")
	}
	return nil
}
func bodyRead(ctx context.Context, f *app.Factory, o *output.BodyOptions) (string, error) {
	cfg, err := f.Config()
	if err != nil {
		return "", err
	}
	return o.Read(ctx, output.BodyContext{Stdin: f.IO.In, Getenv: f.Getenv, ConfigEditor: cfg.Defaults.Editor, CanPrompt: f.IO.CanPrompt()})
}
func page(ctx context.Context, f *app.Factory, disabled bool, fn func(io.Writer) error) error {
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	return output.ResolvePager(f.Getenv, cfg.Defaults.Pager).Run(ctx, f.IO.Out, f.IO.ErrOut, output.PagerOptions{Disabled: disabled}, fn)
}
func jsonOptions(f *app.Factory) output.WriteOptions {
	return output.WriteOptions{TTY: f.IO.IsStdoutTTY(), Color: f.IO.ColorEnabled(), Width: f.IO.TerminalWidth()}
}
func line(s string) string { return output.Sanitize(strings.Join(strings.Fields(s), " ")) }

func labelIDs(ctx context.Context, c *api.Client, ref reporef.Ref, names []string) ([]int64, error) {
	ids := []int64{}
	if len(names) == 0 {
		return ids, nil
	}
	labels, err := c.ListLabels(ctx, ref.Owner, ref.Name)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		found := false
		for _, l := range labels {
			if l.Name == name {
				ids = append(ids, l.ID)
				found = true
				break
			}
		}
		if !found {
			return nil, fjoerr.New(fjoerr.KindNotFound, "label %q not found in %s", name, ref.FullName())
		}
	}
	return ids, nil
}
func milestoneID(ctx context.Context, c *api.Client, ref reporef.Ref, name string) (int64, error) {
	items, err := c.ListMilestones(ctx, ref.Owner, ref.Name)
	if err != nil {
		return 0, err
	}
	// Match names first, as the issue-list API does, then fall back to IDs.
	for _, m := range items {
		if m.Title == name {
			return m.ID, nil
		}
	}
	for _, m := range items {
		if strconv.FormatInt(m.ID, 10) == name {
			return m.ID, nil
		}
	}
	return 0, fjoerr.New(fjoerr.KindNotFound, "milestone %q not found in %s", name, ref.FullName())
}
func resolveUsers(ctx context.Context, c *api.Client, names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == "@me" {
			u, err := c.CurrentUser(ctx)
			if err != nil {
				return nil, err
			}
			name = u.Login
		}
		if strings.TrimSpace(name) == "" {
			return nil, fjoerr.Usage("assignee cannot be empty")
		}
		out = append(out, name)
	}
	return out, nil
}

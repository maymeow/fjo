// Package pr implements the Forgejo pull request commands.
package pr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
	"github.com/maymeow/fjo/internal/output"
	"github.com/maymeow/fjo/internal/reporef"
)

func NewCmd(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "pr <command>", Short: "Work with Forgejo pull requests"}
	cmd.AddCommand(newListCmd(f), newViewCmd(f), newDiffCmd(f), newChecksCmd(f),
		newCreateCmd(f), newCheckoutCmd(f), newMergeCmd(f), newEditCmd(f),
		newCloseCmd(f), newReopenCmd(f), newReadyCmd(f), newCommentCmd(f))
	return cmd
}

func repoFlag(cmd *cobra.Command, repo *string) {
	cmd.Flags().StringVarP(repo, "repo", "R", "", "Select a repository: [HOST/]OWNER/REPO")
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

func pullURL(c *api.Client, ref reporef.Ref) string {
	return c.InstanceURL() + url.PathEscape(ref.Owner) + "/" + url.PathEscape(ref.Name) + "/pulls"
}

func openBrowser(f *app.Factory, target string) error {
	err := f.Browser(target)
	if errors.Is(err, browser.ErrNoBrowser) {
		_, err = fmt.Fprintln(f.IO.Out, target)
	}
	return err
}

func oneLine(s string) string {
	return output.Sanitize(strings.Join(strings.Fields(s), " "))
}

type pullTarget struct {
	repo   reporef.Ref
	client *api.Client
	number int
	pull   *api.PullRequest
}

func (t pullTarget) read(ctx context.Context) (*api.PullRequest, error) {
	if t.pull != nil {
		return t.pull, nil
	}
	return t.client.GetPull(ctx, t.repo.Owner, t.repo.Name, t.number)
}

func resolve(ctx context.Context, f *app.Factory, repo, arg string) (pullTarget, error) {
	var target pullTarget
	var err error
	if strings.Contains(arg, "://") {
		u, parseErr := url.Parse(arg)
		if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" {
			return target, fjoerr.Usage("invalid pull request URL")
		}
		// Accept OWNER/REPO/pulls/NUMBER followed by a tab such as /files or /commits.
		segments := strings.Split(strings.Trim(u.Path, "/"), "/")
		i := len(segments) - 2
		for ; i >= 2; i-- {
			if segments[i] == "pulls" && numeric(segments[i+1]) {
				break
			}
		}
		if i < 2 {
			return target, fjoerr.Usage("expected a pull request URL containing /pulls/NUMBER")
		}
		target.number, err = pullNumber(segments[i+1])
		if err != nil {
			return target, err
		}
		target.repo, err = f.ParseRepo(u.Host + "/" + strings.Join(segments[:i], "/"))
		if err == nil && repo != "" {
			err = checkRepoMatchesURL(f, repo, target.repo)
		}
	} else {
		if arg != "" && (strings.HasPrefix(arg, "#") || numeric(arg)) {
			target.number, err = pullNumber(arg)
			if err != nil {
				return target, err
			}
		}
		target.repo, err = f.ResolveRepo(ctx, repo)
	}
	if err != nil {
		return target, err
	}
	target.client, _, err = f.ClientFor(target.repo.Host)
	if err != nil || target.number > 0 {
		return target, err
	}

	search := api.PullSearch{State: "open", Head: arg}
	if arg == "" {
		search.Head, search.HeadOwner, err = currentHead(ctx, f, target.repo)
		if err != nil {
			return target, err
		}
	} else if owner, branch, ok := strings.Cut(arg, ":"); ok {
		if owner == "" || branch == "" {
			return target, fjoerr.Usage("expected OWNER:BRANCH")
		}
		search.HeadOwner, search.Head = owner, branch
	}
	pulls, err := target.client.ListPulls(ctx, target.repo.Owner, target.repo.Name, search, 2)
	if err != nil {
		return target, err
	}
	switch len(pulls) {
	case 0:
		return target, fjoerr.New(fjoerr.KindNotFound, "no open pull request for branch %q in %s", search.Head, target.repo.FullName())
	case 1:
		target.pull = &pulls[0]
		target.number = target.pull.Number
	default:
		return target, fjoerr.New(fjoerr.KindConflict, "multiple open pull requests for branch %q", search.Head).
			WithHint("specify a pull request number or URL")
	}
	return target, nil
}

func numeric(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func pullNumber(s string) (int, error) {
	value := strings.TrimPrefix(s, "#")
	n, err := strconv.Atoi(value)
	if err != nil || !numeric(value) || n < 1 {
		return 0, fjoerr.Usage("invalid pull request number %q", s)
	}
	return n, nil
}

func currentHead(ctx context.Context, f *app.Factory, base reporef.Ref) (branch, owner string, err error) {
	g := f.Git()
	branch, err = g.CurrentBranch(ctx)
	if err != nil {
		return "", "", app.GitContextError(err)
	}
	// The tracking branch can have a different name in the fork.
	upstream, err := g.Run(ctx, "for-each-ref", "--format=%(refname)%09%(upstream:remotename)%09%(upstream:remoteref)", "--", "refs/heads/"+branch)
	if err != nil {
		return "", "", app.GitContextError(err)
	}
	for _, line := range strings.Split(upstream, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 3 || parts[0] != "refs/heads/"+branch || parts[1] == "" {
			continue
		}
		remotes, err := g.Remotes(ctx)
		if err != nil {
			return "", "", app.GitContextError(err)
		}
		for _, remote := range remotes {
			if remote.Name != parts[1] {
				continue
			}
			parsed, err := git.ParseURL(remote.FetchURL)
			if err != nil {
				break
			}
			cfg, err := f.Config()
			if err != nil {
				return "", "", err
			}
			ref, err := reporef.FromURL(parsed, cfg)
			if err == nil && ref.Host == base.Host {
				return strings.TrimPrefix(parts[2], "refs/heads/"), ref.Owner, nil
			}
		}
		break
	}
	return branch, base.Owner, nil
}

// checkRepoMatchesURL rejects --repo when it names a different repository
// than the pull request URL. The host is compared only when --repo sets one.
func checkRepoMatchesURL(f *app.Factory, repo string, fromURL reporef.Ref) error {
	flag, err := f.ParseRepo(repo)
	if err != nil {
		return err
	}
	sameHost := strings.Count(repo, "/") < 2 || strings.EqualFold(flag.Host, fromURL.Host)
	if !sameHost || !strings.EqualFold(flag.Owner, fromURL.Owner) || !strings.EqualFold(flag.Name, fromURL.Name) {
		return fjoerr.Usage("--repo %s does not match the pull request URL (%s)", repo, fromURL.FullName())
	}
	return nil
}

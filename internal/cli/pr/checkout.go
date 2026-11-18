package pr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
	"github.com/maymeow/fjo/internal/reporef"
)

func newCheckoutCmd(f *app.Factory) *cobra.Command {
	var repo, branch string
	var force bool
	cmd := &cobra.Command{Use: "checkout <number|url>", Short: "Check out a pull request locally", Args: cobra.ExactArgs(1)}
	repoFlag(cmd, &repo)
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Name of the local branch")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Allow replacing a diverged local branch")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		target, pull, err := readTarget(ctx, f, repo, args[0])
		if err != nil {
			return err
		}
		name := branch
		if name == "" {
			name = pull.Head.Ref
		}
		return checkout(ctx, f, target, pull, name, force)
	}
	return cmd
}

func validBranch(ctx context.Context, g *git.Client, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") || branch == "HEAD" {
		return fjoerr.Usage("invalid branch name %q", branch)
	}
	if _, err := g.Run(ctx, "check-ref-format", "refs/heads/"+branch); err != nil {
		return fjoerr.Wrap(fjoerr.KindUsage, err, "invalid branch name %q", branch)
	}
	return nil
}

func validSHA(sha string) bool {
	return (len(sha) == 40 || len(sha) == 64) && strings.Trim(sha, "0123456789abcdefABCDEF") == ""
}

func cleanWorktree(ctx context.Context, g *git.Client) error {
	out, err := g.Run(ctx, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return err
	}
	if out != "" {
		return fjoerr.New(fjoerr.KindConflict, "working tree is dirty; commit or stash your changes before checkout")
	}
	return nil
}

func checkout(ctx context.Context, f *app.Factory, target pullTarget, pull *api.PullRequest, name string, force bool) error {
	g := f.Git()
	if err := validBranch(ctx, g, name); err != nil {
		return err
	}
	if !validSHA(pull.Head.SHA) {
		return fjoerr.New(fjoerr.KindConflict, "pull request #%d has no valid head commit", target.number)
	}
	remotes, err := f.RemoteRepos(ctx, nil)
	if err != nil {
		return err
	}
	baseRemote := remoteFor(remotes, target.repo)
	if baseRemote == "" {
		return fjoerr.Usage("no local git remote matches %s", target.repo)
	}
	pullRef := fmt.Sprintf("refs/pull/%d/head", target.number)
	if err := checkoutFrom(ctx, f, baseRemote, pullRef, name, pull.Head.SHA, force); err != nil {
		return err
	}
	head, err := headRepository(pull, target.repo)
	if err != nil || head.Equal(target.repo) {
		return nil
	}
	forkRemote := remoteFor(remotes, head)
	if forkRemote == "" || !hasTrackingBranch(ctx, g, forkRemote, pull.Head.Ref) {
		return nil
	}
	_, err = g.Run(ctx, "branch", "--set-upstream-to="+forkRemote+"/"+pull.Head.Ref, "--", name)
	return err
}

func checkoutFrom(ctx context.Context, f *app.Factory, remote, pullRef, name, expected string, force bool) error {
	g := f.Git()
	if err := cleanWorktree(ctx, g); err != nil {
		return err
	}
	ref := "refs/heads/" + name
	previous, err := g.Run(ctx, "rev-parse", "--verify", "--quiet", ref)
	exists := err == nil
	var gitErr *git.Error
	if err != nil && (!errors.As(err, &gitErr) || gitErr.ExitCode != 1) {
		return err
	}
	if err := g.RunWithIO(ctx, io.Discard, f.IO.ErrOut, "fetch", "--no-tags", "--", remote, pullRef); err != nil {
		return err
	}
	fetched, err := g.Run(ctx, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return err
	}
	sha := strings.TrimSpace(fetched)
	if !strings.EqualFold(sha, expected) {
		return fjoerr.New(fjoerr.KindConflict, "pull request head changed from %s to %s during checkout; retry", expected, sha)
	}
	if exists && !force {
		if _, err := g.Run(ctx, "merge-base", "--is-ancestor", strings.TrimSpace(previous), sha); err != nil {
			if errors.As(err, &gitErr) && gitErr.ExitCode == 1 {
				return fjoerr.New(fjoerr.KindConflict, "local branch %q has diverged; use --force to replace it", name)
			}
			return err
		}
	}
	if err := cleanWorktree(ctx, g); err != nil {
		return err
	}
	if err := switchToPull(ctx, f, name, sha, exists, force); err != nil {
		return err
	}
	return nil
}

func switchToPull(ctx context.Context, f *app.Factory, name, sha string, exists, force bool) error {
	g := f.Git()
	if exists && !force {
		if err := g.RunWithIO(ctx, f.IO.Out, f.IO.ErrOut, "checkout", "--no-overwrite-ignore", name, "--"); err != nil {
			return err
		}
		return g.RunWithIO(ctx, f.IO.Out, f.IO.ErrOut, "merge", "--ff-only", "--no-overwrite-ignore", "--", sha)
	}
	mode := "-b"
	if exists {
		mode = "-B"
	}
	return g.RunWithIO(ctx, f.IO.Out, f.IO.ErrOut, "checkout", "--no-overwrite-ignore", "--no-track", mode, name, sha, "--")
}

func remoteFor(remotes []app.RemoteRepo, ref reporef.Ref) string {
	for _, remote := range remotes {
		if remote.Repo.Equal(ref) {
			return remote.Remote.Name
		}
	}
	return ""
}

func hasTrackingBranch(ctx context.Context, g *git.Client, remote, branch string) bool {
	_, err := g.Run(ctx, "show-ref", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch)
	return err == nil
}

func deleteLocalHead(ctx context.Context, f *app.Factory, base reporef.Ref, pull *api.PullRequest) {
	head, err := headRepository(pull, base)
	if err != nil || !validSHA(pull.Head.SHA) {
		return
	}
	g := f.Git()
	current, err := g.CurrentBranch(ctx)
	if err != nil {
		return
	}
	if current == pull.Head.Ref || validBranch(ctx, g, pull.Head.Ref) != nil {
		return
	}
	remotes, err := f.RemoteRepos(ctx, nil)
	if err != nil || remoteFor(remotes, head) == "" {
		return
	}
	tip, err := g.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+pull.Head.Ref)
	if err != nil || strings.TrimSpace(tip) != pull.Head.SHA {
		return
	}
	if _, err := g.Run(ctx, "branch", "-d", "--", pull.Head.Ref); err != nil {
		fmt.Fprintf(f.IO.ErrOut, "Could not delete local branch %s: %s\n", oneLine(pull.Head.Ref), oneLine(err.Error()))
	}
}

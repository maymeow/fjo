package pr

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
	"github.com/maymeow/fjo/internal/reporef"
)

func readBody(ctx context.Context, f *app.Factory, opts *output.BodyOptions) (string, error) {
	cfg, err := f.Config()
	if err != nil {
		return "", err
	}
	return opts.Read(ctx, output.BodyContext{
		Stdin: f.IO.In, Getenv: f.Getenv, ConfigEditor: cfg.Defaults.Editor, CanPrompt: f.IO.CanPrompt(),
	})
}

func userLogins(ctx context.Context, c *api.Client, names []string) ([]string, error) {
	logins := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fjoerr.Usage("user login cannot be empty")
		}
		if name == "@me" {
			user, err := c.CurrentUser(ctx)
			if err != nil {
				return nil, err
			}
			name = user.Login
		}
		logins = append(logins, name)
	}
	return logins, nil
}

func findMilestone(ctx context.Context, target pullTarget, value string) (int64, error) {
	milestones, err := target.client.ListMilestones(ctx, target.repo.Owner, target.repo.Name)
	if err != nil {
		return 0, err
	}
	for _, milestone := range milestones {
		if milestone.Title == value {
			return milestone.ID, nil
		}
	}
	for _, milestone := range milestones {
		if fmt.Sprint(milestone.ID) == value {
			return milestone.ID, nil
		}
	}
	return 0, fjoerr.New(fjoerr.KindNotFound, "milestone %q not found in %s", value, target.repo.FullName())
}

func newCloseCmd(f *app.Factory) *cobra.Command {
	var repo, comment string
	var deleteBranch bool
	cmd := &cobra.Command{Use: "close <number|url>", Short: "Close a pull request", Args: cobra.ExactArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.Flags().StringVarP(&comment, "comment", "c", "", "Add a comment after closing")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "Delete the head branch after closing")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("comment") && strings.TrimSpace(comment) == "" {
			return fjoerr.Usage("comment cannot be empty")
		}
		ctx := cmd.Context()
		target, pull, err := readTarget(ctx, f, repo, args[0])
		if err != nil {
			return err
		}
		if pull.IsMerged() {
			return fjoerr.New(fjoerr.KindConflict, "pull request #%d is already merged", target.number)
		}
		if deleteBranch {
			if _, err := headRepository(pull, target.repo); err != nil {
				return err
			}
		}
		if err := target.edit(ctx, api.EditPullOptions{State: pointer("closed")}); err != nil {
			return err
		}
		if comment != "" {
			if _, err := target.client.CreateIssueComment(ctx, target.repo.Owner, target.repo.Name, target.number, comment); err != nil {
				return fjoerr.Wrap(fjoerr.KindOf(err), err, "pull request closed but adding the comment failed")
			}
		}
		if deleteBranch {
			if err := deleteRemoteHead(ctx, target, pull); err != nil {
				return fjoerr.Wrap(fjoerr.KindOf(err), err, "pull request closed but deleting the head branch failed")
			}
			deleteLocalHead(ctx, f, target.repo, pull)
		}
		fmt.Fprintf(f.IO.ErrOut, "Closed pull request #%d\n", target.number)
		return nil
	}
	return cmd
}

func newReopenCmd(f *app.Factory) *cobra.Command {
	var repo string
	cmd := &cobra.Command{Use: "reopen <number|url>", Short: "Reopen a pull request", Args: cobra.ExactArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		target, pull, err := readTarget(ctx, f, repo, args[0])
		if err != nil {
			return err
		}
		if pull.IsMerged() {
			return fjoerr.New(fjoerr.KindConflict, "pull request #%d is already merged", target.number)
		}
		return target.edit(ctx, api.EditPullOptions{State: pointer("open")})
	}
	return cmd
}

func newReadyCmd(f *app.Factory) *cobra.Command {
	var repo string
	var undo bool
	cmd := &cobra.Command{Use: "ready [<number|url|branch>]", Short: "Mark a pull request ready for review", Args: cobra.MaximumNArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.Flags().BoolVar(&undo, "undo", false, "Mark the pull request as a draft")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		target, pull, err := readTarget(ctx, f, repo, firstArg(args))
		if err != nil {
			return err
		}
		title := readyTitle(pull.Title)
		if title == "" {
			return fjoerr.Usage("pull request must have a title after removing the draft prefix")
		}
		if undo {
			title = "WIP: " + title
		}
		if pull.Title == title {
			return nil
		}
		return target.edit(ctx, api.EditPullOptions{Title: &title})
	}
	return cmd
}

func readyTitle(title string) string {
	for {
		upper := strings.ToUpper(title)
		switch {
		case strings.HasPrefix(upper, "WIP:"):
			title = strings.TrimSpace(title[len("WIP:"):])
		case strings.HasPrefix(upper, "[WIP]"):
			title = strings.TrimSpace(title[len("[WIP]"):])
		default:
			return title
		}
	}
}

func newCommentCmd(f *app.Factory) *cobra.Command {
	var repo string
	var body output.BodyOptions
	cmd := &cobra.Command{Use: "comment <number|url>", Short: "Comment on a pull request", Args: cobra.ExactArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	output.AddBodyFlags(cmd, &body)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !body.Provided() && !f.IO.CanPrompt() {
			return fjoerr.Usage("provide --body or --body-file without a terminal")
		}
		ctx := cmd.Context()
		target, _, err := readTarget(ctx, f, repo, args[0])
		if err != nil {
			return err
		}
		text, err := readBody(ctx, f, &body)
		if err != nil {
			return err
		}
		if !body.Provided() {
			text, err = f.Prompter.Input("Comment:")
			if err != nil {
				return err
			}
		}
		if strings.TrimSpace(text) == "" {
			return fjoerr.Usage("comment body cannot be empty")
		}
		comment, err := target.client.CreateIssueComment(ctx, target.repo.Owner, target.repo.Name, target.number, text)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f.IO.Out, cmdutil.OneLine(comment.HTMLURL))
		return err
	}
	return cmd
}

func pointer[T any](value T) *T { return &value }

func readTarget(ctx context.Context, f *app.Factory, repo, arg string) (pullTarget, *api.PullRequest, error) {
	target, err := resolve(ctx, f, repo, arg)
	if err != nil {
		return target, nil, err
	}
	pull, err := target.client.GetPull(ctx, target.repo.Owner, target.repo.Name, target.number)
	return target, pull, err
}

func (t pullTarget) edit(ctx context.Context, options api.EditPullOptions) error {
	return t.client.EditPull(ctx, t.repo.Owner, t.repo.Name, t.number, options)
}

func headRepository(pull *api.PullRequest, base reporef.Ref) (reporef.Ref, error) {
	if pull.Head.Repo == nil || strings.Count(pull.Head.Repo.FullName, "/") != 1 ||
		pull.Head.Ref == "" || pull.Head.Ref == "." || pull.Head.Ref == ".." {
		return reporef.Ref{}, fjoerr.New(fjoerr.KindConflict, "pull request head repository or branch is unavailable")
	}
	return reporef.Parse(base.Host+"/"+pull.Head.Repo.FullName, nil, nil)
}

func deleteRemoteHead(ctx context.Context, target pullTarget, pull *api.PullRequest) error {
	ref, err := headRepository(pull, target.repo)
	if err != nil {
		return err
	}
	sha, err := target.client.GetBranchSHA(ctx, ref.Owner, ref.Name, pull.Head.Ref)
	if fjoerr.KindOf(err) == fjoerr.KindNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if sha == "" || sha != pull.Head.SHA {
		return fjoerr.New(fjoerr.KindConflict, "head branch has moved since the pull request was read")
	}
	err = target.client.DeleteBranch(ctx, ref.Owner, ref.Name, pull.Head.Ref)
	if fjoerr.KindOf(err) == fjoerr.KindNotFound {
		return nil
	}
	return err
}

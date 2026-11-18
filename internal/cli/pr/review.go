package pr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

type reviewKind struct {
	flag, short, label, event, done string
	needsBody                       bool
}

var reviewKinds = []reviewKind{
	{flag: "approve", short: "a", label: "Approve", event: api.ReviewApprove, done: "Approved"},
	{flag: "request-changes", short: "r", label: "Request changes", event: api.ReviewRequestChanges, done: "Requested changes to", needsBody: true},
	{flag: "comment", short: "c", label: "Comment", event: api.ReviewComment, done: "Reviewed", needsBody: true},
}

func newReviewCmd(f *app.Factory) *cobra.Command {
	var repo string
	var body output.BodyOptions
	var selected [3]bool
	cmd := &cobra.Command{
		Use:   "review [<number|url|branch>]",
		Short: "Add a review to a pull request",
		Long: `Approve a pull request, request changes or leave a review comment.

Without an argument the pull request for the current branch is reviewed.
--request-changes and --comment require a body. Without a review type flag
fjo asks for one in a terminal and fails otherwise.`,
		Example: `  fjo pr review 15 --approve
  fjo pr review 15 --request-changes --body "Please add tests"
  fjo pr review --comment --body-file review.md`,
		Args: cobra.MaximumNArgs(1),
	}
	cmdutil.AddRepoFlag(cmd, &repo)
	for i, kind := range reviewKinds {
		cmd.Flags().BoolVarP(&selected[i], kind.flag, kind.short, false, kind.label+" the pull request")
	}
	output.AddBodyFlags(cmd, &body)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		kind, chosen, err := chosenReviewKind(selected[:])
		if err != nil {
			return err
		}
		canPrompt := f.IO.CanPrompt()
		if !chosen && !canPrompt {
			return fjoerr.Usage("choose a review type: --approve, --request-changes or --comment")
		}
		if chosen && kind.needsBody && !body.Provided() && !canPrompt {
			return fjoerr.Usage("--%s requires --body or --body-file without a terminal", kind.flag)
		}
		ctx := cmd.Context()
		target, pull, err := readTarget(ctx, f, repo, firstArg(args))
		if err != nil {
			return err
		}
		supported, err := target.client.SupportsPullReviews(ctx, target.repo.Owner, target.repo.Name, target.number)
		if err != nil {
			return err
		}
		if !supported {
			return fjoerr.New(fjoerr.KindUnsupported, "%s does not support pull request reviews", target.repo.Host)
		}
		if !chosen {
			if kind, err = promptReviewKind(f); err != nil {
				return err
			}
		}
		text, err := readBody(ctx, f, &body)
		if err != nil {
			return err
		}
		if kind.needsBody && !body.Provided() {
			if text, err = f.Prompter.Input("Review body:"); err != nil {
				return err
			}
		}
		if kind.needsBody && strings.TrimSpace(text) == "" {
			return fjoerr.Usage("--%s requires a non-empty body", kind.flag)
		}
		_, err = target.client.CreatePullReview(ctx, target.repo.Owner, target.repo.Name, target.number, api.CreatePullReviewOptions{
			Event: kind.event, Body: text, CommitID: pull.Head.SHA,
		})
		if err != nil {
			var apiErr *api.APIError
			if errors.As(err, &apiErr) {
				return fjoerr.Wrap(fjoerr.KindOf(err), err, "could not review pull request #%d", target.number)
			}
			return err
		}
		_, err = fmt.Fprintf(f.IO.ErrOut, "%s pull request #%d\n", kind.done, target.number)
		return err
	}
	return cmd
}

func chosenReviewKind(selected []bool) (reviewKind, bool, error) {
	var kind reviewKind
	chosen := false
	for i, on := range selected {
		if !on {
			continue
		}
		if chosen {
			return kind, false, fjoerr.Usage("choose only one of --approve, --request-changes and --comment")
		}
		kind, chosen = reviewKinds[i], true
	}
	return kind, chosen, nil
}

func promptReviewKind(f *app.Factory) (reviewKind, error) {
	labels := make([]string, len(reviewKinds))
	for i, kind := range reviewKinds {
		labels[i] = kind.label
	}
	index, err := f.Prompter.Select("Review type:", labels)
	if err != nil {
		return reviewKind{}, err
	}
	return reviewKinds[index], nil
}

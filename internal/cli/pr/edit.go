package pr

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

type editOptions struct {
	repo, title, base, milestone                                                          string
	removeMilestone                                                                       bool
	body                                                                                  output.BodyOptions
	addLabels, removeLabels, addAssignees, removeAssignees, addReviewers, removeReviewers []string
}

func newEditCmd(f *app.Factory) *cobra.Command {
	var opts editOptions
	cmd := &cobra.Command{Use: "edit <number|url>", Short: "Edit a pull request", Args: cobra.ExactArgs(1)}
	repoFlag(cmd, &opts.repo)
	output.AddBodyFlags(cmd, &opts.body)
	flags := cmd.Flags()
	flags.StringVarP(&opts.title, "title", "t", "", "Set the title")
	flags.StringVarP(&opts.base, "base", "B", "", "Set the target branch")
	flags.StringVarP(&opts.milestone, "milestone", "m", "", "Set milestone by title or ID")
	flags.BoolVar(&opts.removeMilestone, "remove-milestone", false, "Clear the milestone")
	flags.StringSliceVar(&opts.addLabels, "add-label", nil, "Add labels by name")
	flags.StringSliceVar(&opts.removeLabels, "remove-label", nil, "Remove labels by name")
	flags.StringSliceVar(&opts.addAssignees, "add-assignee", nil, "Add assignees (or @me)")
	flags.StringSliceVar(&opts.removeAssignees, "remove-assignee", nil, "Remove assignees (or @me)")
	flags.StringSliceVar(&opts.addReviewers, "add-reviewer", nil, "Request reviews by user login")
	flags.StringSliceVar(&opts.removeReviewers, "remove-reviewer", nil, "Remove requested reviewers")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		payload, err := editFields(cmd, f, &opts)
		if err != nil {
			return err
		}
		target, current, err := readTarget(ctx, f, opts.repo, args[0])
		if err != nil {
			return err
		}
		if err := applyEdit(ctx, f, target, current, &opts, payload); err != nil {
			return err
		}
		fmt.Fprintf(f.IO.ErrOut, "Updated pull request #%d\n", target.number)
		return nil
	}
	return cmd
}

func editFields(cmd *cobra.Command, f *app.Factory, opts *editOptions) (api.EditPullOptions, error) {
	var payload api.EditPullOptions
	flags := cmd.Flags()
	if flags.Changed("milestone") && (opts.removeMilestone || opts.milestone == "") {
		return payload, fjoerr.Usage("--milestone requires a value and cannot be combined with --remove-milestone")
	}
	if flags.Changed("title") {
		if strings.TrimSpace(opts.title) == "" {
			return payload, fjoerr.Usage("title cannot be empty")
		}
		payload.Title = &opts.title
	}
	if flags.Changed("base") {
		if err := validBranch(cmd.Context(), f.Git(), opts.base); err != nil {
			return payload, err
		}
		payload.Base = &opts.base
	}
	if opts.removeMilestone {
		payload.Milestone = pointer(int64(0))
	}
	if payload == (api.EditPullOptions{}) && !opts.body.Provided() && opts.milestone == "" &&
		len(opts.addLabels)+len(opts.removeLabels)+len(opts.addAssignees)+len(opts.removeAssignees)+len(opts.addReviewers)+len(opts.removeReviewers) == 0 {
		return payload, fjoerr.Usage("specify at least one field to edit")
	}
	return payload, nil
}

func applyEdit(ctx context.Context, f *app.Factory, target pullTarget, current *api.PullRequest, opts *editOptions, payload api.EditPullOptions) error {
	owner, repo := target.repo.Owner, target.repo.Name
	c := target.client
	if opts.body.Provided() {
		body, err := readBody(ctx, f, &opts.body)
		if err != nil {
			return err
		}
		payload.Body = &body
	}
	if opts.milestone != "" {
		id, err := findMilestone(ctx, target, opts.milestone)
		if err != nil {
			return err
		}
		payload.Milestone = &id
	}
	addLabels, err := c.ResolveLabelIDs(ctx, owner, repo, opts.addLabels)
	if err != nil {
		return err
	}
	removeLabels, err := c.ResolveLabelIDs(ctx, owner, repo, opts.removeLabels)
	if err != nil {
		return err
	}
	for _, id := range addLabels {
		if slices.Contains(removeLabels, id) {
			return fjoerr.Usage("cannot add and remove the same label")
		}
	}
	addAssignees, removeAssignees, err := resolveUserChanges(ctx, c, opts.addAssignees, opts.removeAssignees)
	if err != nil {
		return err
	}
	addReviewers, removeReviewers, err := resolveUserChanges(ctx, c, opts.addReviewers, opts.removeReviewers)
	if err != nil {
		return err
	}
	if len(addAssignees)+len(removeAssignees) > 0 {
		payload.Assignees = pointer(updatedAssignees(current, addAssignees, removeAssignees))
	}
	if payload != (api.EditPullOptions{}) {
		if err := target.edit(ctx, payload); err != nil {
			return err
		}
	}
	if len(addLabels) > 0 {
		if err := c.AddIssueLabels(ctx, owner, repo, target.number, addLabels); err != nil {
			return partialEdit(err, "adding labels")
		}
	}
	for _, id := range removeLabels {
		if err := c.RemoveIssueLabel(ctx, owner, repo, target.number, id); err != nil && fjoerr.KindOf(err) != fjoerr.KindNotFound {
			return partialEdit(err, "removing a label")
		}
	}
	if len(addReviewers) > 0 {
		if err := c.ChangePullReviewers(ctx, owner, repo, target.number, addReviewers, false); err != nil {
			return partialEdit(err, "adding reviewers")
		}
	}
	if len(removeReviewers) > 0 {
		if err := c.ChangePullReviewers(ctx, owner, repo, target.number, removeReviewers, true); err != nil {
			return partialEdit(err, "removing reviewers")
		}
	}
	return nil
}

func resolveUserChanges(ctx context.Context, c *api.Client, add, remove []string) ([]string, []string, error) {
	add, err := userLogins(ctx, c, add)
	if err != nil {
		return nil, nil, err
	}
	remove, err = userLogins(ctx, c, remove)
	if err != nil {
		return nil, nil, err
	}
	for _, user := range add {
		if containsLogin(remove, user) {
			return nil, nil, fjoerr.Usage("cannot add and remove the same user %q", user)
		}
	}
	return add, remove, nil
}

func containsLogin(names []string, name string) bool {
	return slices.ContainsFunc(names, func(value string) bool { return strings.EqualFold(value, name) })
}

func updatedAssignees(current *api.PullRequest, add, remove []string) []string {
	logins := []string{}
	users := current.Assignees
	if len(users) == 0 && current.Assignee != nil {
		users = []api.User{*current.Assignee}
	}
	for _, user := range users {
		if !containsLogin(remove, user.Login) {
			logins = append(logins, user.Login)
		}
	}
	for _, user := range add {
		if !containsLogin(logins, user) {
			logins = append(logins, user)
		}
	}
	return logins
}

func partialEdit(err error, step string) error {
	return fjoerr.Wrap(fjoerr.KindOf(err), err, "pull request may be partially updated; %s failed", step)
}

package issue

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
	"github.com/spf13/cobra"
)

func newCreateCmd(f *app.Factory) *cobra.Command {
	var repo, title, milestone string
	var labels, assignees []string
	var body output.BodyOptions
	var web bool
	cmd := &cobra.Command{Use: "create", Short: "Create an issue", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		ref, err := f.ResolveRepo(ctx, repo)
		if err != nil {
			return err
		}
		if !web && strings.TrimSpace(title) == "" {
			if !f.IO.CanPrompt() {
				return fjoerr.Usage("--title is required without a terminal")
			}
			title, err = f.Prompter.Input("Title:")
			if err != nil {
				return err
			}
			if strings.TrimSpace(title) == "" {
				return fjoerr.Usage("title cannot be empty")
			}
		}
		text, err := bodyRead(ctx, f, &body)
		if err != nil {
			return err
		}
		if !web && !body.Provided() && f.IO.CanPrompt() {
			text, err = f.Prompter.Input("Body:")
			if err != nil {
				return err
			}
		}
		if web {
			// Forgejo's browser form accepts title and body; metadata must be selected there.
			if len(labels) > 0 || len(assignees) > 0 || cmd.Flags().Changed("milestone") {
				return fjoerr.New(fjoerr.KindUnsupported, "--web cannot prefill labels, assignees or a milestone; select them in the browser")
			}
			q := url.Values{}
			if title != "" {
				q.Set("title", title)
			}
			if text != "" {
				q.Set("body", text)
			}
			u := webURL(f, ref, "issues/new")
			if len(q) > 0 {
				u += "?" + q.Encode()
			}
			return openBrowser(f, u)
		}
		c, _, err := f.ClientFor(ref.Host)
		if err != nil {
			return err
		}
		ids, err := c.ResolveLabelIDs(ctx, ref.Owner, ref.Name, labels)
		if err != nil {
			return err
		}
		users, err := resolveUsers(ctx, c, assignees)
		if err != nil {
			return err
		}
		var mid int64
		if cmd.Flags().Changed("milestone") {
			mid, err = milestoneID(ctx, c, ref, milestone)
			if err != nil {
				return err
			}
		}
		v, err := c.CreateIssue(ctx, ref.Owner, ref.Name, api.CreateIssueOptions{Title: title, Body: text, Labels: ids, Assignees: users, Milestone: mid})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f.IO.Out, line(v.HTMLURL))
		return err
	}
	repoFlag(cmd, &repo)
	fl := cmd.Flags()
	fl.StringVarP(&title, "title", "t", "", "Issue title")
	output.AddBodyFlags(cmd, &body)
	fl.StringSliceVarP(&labels, "label", "l", nil, "Add labels by name")
	fl.StringSliceVarP(&assignees, "assignee", "a", nil, "Assign users (or @me)")
	fl.StringVarP(&milestone, "milestone", "m", "", "Milestone title or ID")
	fl.BoolVarP(&web, "web", "w", false, "Open the creation form in the browser")
	return cmd
}

func newActionCmd(f *app.Factory, action string) *cobra.Command {
	var repo, comment, reason string
	var yes bool
	var body output.BodyOptions
	cmd := &cobra.Command{Use: action + " <number|url>", Short: strings.ToUpper(action[:1]) + action[1:] + " an issue", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if action == "close" && cmd.Flags().Changed("reason") {
			return fjoerr.New(fjoerr.KindUnsupported, "Forgejo does not support issue close reasons")
		}
		ref, n, err := resolve(ctx, f, repo, args[0])
		if err != nil {
			return err
		}
		var text string
		if action == "comment" {
			if !body.Provided() && !f.IO.CanPrompt() {
				return fjoerr.Usage("provide --body or --body-file without a terminal")
			}
			text, err = bodyRead(ctx, f, &body)
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
		}
		if action == "delete" && !yes {
			if !f.IO.CanPrompt() {
				return fjoerr.Usage("--yes is required without a terminal")
			}
			answer, err := f.Prompter.Input(fmt.Sprintf("Type %d to delete issue %s#%d:", n, ref.FullName(), n))
			if err != nil {
				return err
			}
			if answer != strconv.Itoa(n) {
				return fjoerr.New(fjoerr.KindCancelled, "deletion cancelled")
			}
		}
		c, _, err := f.ClientFor(ref.Host)
		if err != nil {
			return err
		}
		if _, err := getIssue(ctx, c, ref, n); err != nil {
			return err
		}
		switch action {
		case "comment":
			v, err := c.CreateIssueComment(ctx, ref.Owner, ref.Name, n, text)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(f.IO.Out, line(v.HTMLURL))
			return err
		case "close", "reopen":
			if cmd.Flags().Changed("comment") {
				if strings.TrimSpace(comment) == "" {
					return fjoerr.Usage("comment cannot be empty")
				}
				if _, err := c.CreateIssueComment(ctx, ref.Owner, ref.Name, n, comment); err != nil {
					return err
				}
			}
			state := "closed"
			if action == "reopen" {
				state = "open"
			}
			_, err = c.EditIssue(ctx, ref.Owner, ref.Name, n, api.EditIssueOptions{State: &state})
			if err != nil && cmd.Flags().Changed("comment") {
				return fjoerr.Wrap(fjoerr.KindOf(err), err, "comment was added, but changing issue state failed")
			}
		case "delete":
			err = c.DeleteIssue(ctx, ref.Owner, ref.Name, n)
		case "pin", "unpin":
			err = c.PinIssue(ctx, ref.Owner, ref.Name, n, action == "pin")
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(f.IO.ErrOut, "Issue %s#%d: %s\n", ref.FullName(), n, action)
		return nil
	}
	repoFlag(cmd, &repo)
	switch action {
	case "close", "reopen":
		cmd.Flags().StringVarP(&comment, "comment", "c", "", "Add a comment before changing state")
		if action == "close" {
			cmd.Flags().StringVar(&reason, "reason", "", "Unsupported: Forgejo has no close reasons")
		}
	case "comment":
		output.AddBodyFlags(cmd, &body)
	case "delete":
		cmd.Flags().BoolVar(&yes, "yes", false, "Confirm deletion without prompting")
	}
	return cmd
}

func newEditCmd(f *app.Factory) *cobra.Command {
	var repo, title, milestone string
	var addLabels, removeLabels, addUsers, removeUsers []string
	var removeMilestone bool
	var body output.BodyOptions
	cmd := &cobra.Command{Use: "edit <number|url>", Short: "Edit an issue", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		fl := cmd.Flags()
		if fl.Changed("milestone") && removeMilestone {
			return fjoerr.Usage("--milestone and --remove-milestone cannot be used together")
		}
		if fl.Changed("title") && strings.TrimSpace(title) == "" {
			return fjoerr.Usage("title cannot be empty")
		}
		if !fl.Changed("title") && !body.Provided() && len(addLabels)+len(removeLabels)+len(addUsers)+len(removeUsers) == 0 && !fl.Changed("milestone") && !removeMilestone {
			return fjoerr.Usage("specify at least one field to edit")
		}
		// Check that the issue exists before reading the body, so that an
		// editor or stdin is not used for an edit that will fail anyway.
		ref, n, err := resolve(ctx, f, repo, args[0])
		if err != nil {
			return err
		}
		c, _, err := f.ClientFor(ref.Host)
		if err != nil {
			return err
		}
		current, err := getIssue(ctx, c, ref, n)
		if err != nil {
			return err
		}
		text, err := bodyRead(ctx, f, &body)
		if err != nil {
			return err
		}
		addIDs, err := c.ResolveLabelIDs(ctx, ref.Owner, ref.Name, addLabels)
		if err != nil {
			return err
		}
		removeIDs, err := c.ResolveLabelIDs(ctx, ref.Owner, ref.Name, removeLabels)
		if err != nil {
			return err
		}
		for _, id := range addIDs {
			if slices.Contains(removeIDs, id) {
				return fjoerr.Usage("cannot add and remove the same label")
			}
		}
		add, err := resolveUsers(ctx, c, addUsers)
		if err != nil {
			return err
		}
		remove, err := resolveUsers(ctx, c, removeUsers)
		if err != nil {
			return err
		}
		for _, name := range add {
			if slices.Contains(remove, name) {
				return fjoerr.Usage("cannot add and remove the same assignee")
			}
		}
		opt := api.EditIssueOptions{}
		if fl.Changed("title") {
			opt.Title = &title
		}
		if body.Provided() {
			opt.Body = &text
		}
		if removeMilestone {
			zero := int64(0)
			opt.Milestone = &zero
		}
		if fl.Changed("milestone") {
			id, err := milestoneID(ctx, c, ref, milestone)
			if err != nil {
				return err
			}
			opt.Milestone = &id
		}
		if len(add)+len(remove) > 0 {
			users := []string{}
			for _, u := range current.Assignees {
				if !slices.Contains(remove, u.Login) {
					users = append(users, u.Login)
				}
			}
			for _, u := range add {
				if !slices.Contains(users, u) {
					users = append(users, u)
				}
			}
			opt.Assignees = &users
		}
		if opt.Title != nil || opt.Body != nil || opt.Milestone != nil || opt.Assignees != nil {
			if _, err = c.EditIssue(ctx, ref.Owner, ref.Name, n, opt); err != nil {
				return err
			}
		}
		if len(addIDs) > 0 {
			if err = c.AddIssueLabels(ctx, ref.Owner, ref.Name, n, addIDs); err != nil {
				return fjoerr.Wrap(fjoerr.KindOf(err), err, "issue fields may have changed, but adding labels failed")
			}
		}
		for _, id := range removeIDs {
			if err = c.RemoveIssueLabel(ctx, ref.Owner, ref.Name, n, id); err != nil {
				return fjoerr.Wrap(fjoerr.KindOf(err), err, "issue may be partially updated; removing a label failed")
			}
		}
		fmt.Fprintf(f.IO.ErrOut, "Updated issue %s#%d\n", ref.FullName(), n)
		return nil
	}
	repoFlag(cmd, &repo)
	fl := cmd.Flags()
	fl.StringVarP(&title, "title", "t", "", "Set the title")
	output.AddBodyFlags(cmd, &body)
	fl.StringSliceVar(&addLabels, "add-label", nil, "Add labels by name")
	fl.StringSliceVar(&removeLabels, "remove-label", nil, "Remove labels by name")
	fl.StringSliceVar(&addUsers, "add-assignee", nil, "Add assignees (or @me)")
	fl.StringSliceVar(&removeUsers, "remove-assignee", nil, "Remove assignees (or @me)")
	fl.StringVarP(&milestone, "milestone", "m", "", "Set milestone by title or ID")
	fl.BoolVar(&removeMilestone, "remove-milestone", false, "Clear the milestone")
	return cmd
}

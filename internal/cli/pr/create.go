package pr

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
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
	"github.com/maymeow/fjo/internal/output"
	"github.com/maymeow/fjo/internal/reporef"
)

type createOptions struct {
	repo, base, head, title, milestone string
	labels, assignees, reviewers       []string
	body                               output.BodyOptions
	fill, fillFirst, draft, push, web  bool
}

func newCreateCmd(f *app.Factory) *cobra.Command {
	var opts createOptions
	cmd := &cobra.Command{Use: "create", Short: "Create a pull request", Args: cobra.NoArgs}
	repoFlag(cmd, &opts.repo)
	flags := cmd.Flags()
	flags.StringVarP(&opts.base, "base", "B", "", "Target branch (defaults to the repository default)")
	flags.StringVarP(&opts.head, "head", "H", "", "Head branch, optionally OWNER:BRANCH")
	flags.StringVarP(&opts.title, "title", "t", "", "Pull request title")
	flags.StringVarP(&opts.milestone, "milestone", "m", "", "Milestone title or ID")
	flags.StringSliceVarP(&opts.labels, "label", "l", nil, "Add labels by name")
	flags.StringSliceVarP(&opts.assignees, "assignee", "a", nil, "Assign users (or @me)")
	flags.StringSliceVarP(&opts.reviewers, "reviewer", "r", nil, "Request reviews by user login")
	flags.BoolVarP(&opts.fill, "fill", "f", false, "Fill from commits on the head branch")
	flags.BoolVar(&opts.fillFirst, "fill-first", false, "Fill from the first commit on the head branch")
	flags.BoolVarP(&opts.draft, "draft", "d", false, "Create a draft pull request")
	flags.BoolVar(&opts.push, "push", false, "Push the current branch if the head branch is missing")
	flags.BoolVarP(&opts.web, "web", "w", false, "Open the comparison in the browser")
	output.AddBodyFlags(cmd, &opts.body)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return createPull(cmd.Context(), f, &opts)
	}
	return cmd
}

func createPull(ctx context.Context, f *app.Factory, opts *createOptions) error {
	if opts.fill && opts.fillFirst {
		return fjoerr.Usage("--fill and --fill-first cannot be combined")
	}
	if opts.title == "" && !opts.fill && !opts.fillFirst && !opts.web && !f.IO.CanPrompt() {
		return fjoerr.Usage("provide --title, --fill or --fill-first without a terminal")
	}
	if opts.web && (len(opts.labels)+len(opts.assignees)+len(opts.reviewers) > 0 || opts.milestone != "") {
		return fjoerr.New(fjoerr.KindUnsupported, "set labels, assignees, reviewers and milestones in the browser when using --web")
	}
	ref, err := f.ResolveRepo(ctx, opts.repo)
	if err != nil {
		return err
	}
	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	target := pullTarget{repo: ref, client: client}
	base := opts.base
	if base == "" {
		settings, err := client.GetRepository(ctx, ref.Owner, ref.Name)
		if err != nil {
			return err
		}
		base = settings.DefaultBranch
		if base == "" {
			return fjoerr.Usage("could not determine the target branch; specify --base")
		}
	}
	g := f.Git()
	if err := validBranch(ctx, g, base); err != nil {
		return err
	}
	head, err := findCreateHead(ctx, f, ref, opts.head)
	if err != nil {
		return err
	}
	if head.repo.Equal(ref) && head.branch == base {
		return fjoerr.Usage("base and head must be different branches")
	}
	search := api.PullSearch{State: "open", Base: base, Head: head.branch, HeadOwner: head.repo.Owner}
	if err := rejectDuplicate(ctx, target, head.repo, search); err != nil {
		return err
	}
	headSHA, err := client.GetBranchSHA(ctx, head.repo.Owner, head.repo.Name, head.branch)
	if err != nil && fjoerr.KindOf(err) != fjoerr.KindNotFound {
		return err
	}
	if err == nil && !validSHA(headSHA) {
		return fjoerr.New(fjoerr.KindConflict, "head branch has no valid commit SHA")
	}
	head.sha = headSHA
	payload, err := createPayload(ctx, f, target, opts, head, base)
	if err != nil {
		return err
	}
	reviewers, err := userLogins(ctx, client, opts.reviewers)
	if err != nil {
		return err
	}
	if err := ensureHeadOnServer(ctx, f, head, opts.push); err != nil {
		return err
	}
	if opts.web {
		query := url.Values{"title": {payload.Title}, "body": {payload.Body}}
		path := strings.TrimSuffix(pullURL(client, ref), "/pulls") + "/compare/" + url.PathEscape(base) + "..." + url.PathEscape(payload.Head)
		return openBrowser(f, path+"?"+query.Encode())
	}
	created, err := client.CreatePull(ctx, ref.Owner, ref.Name, payload)
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindConflict {
			if duplicate := rejectDuplicate(ctx, target, head.repo, search); fjoerr.KindOf(duplicate) == fjoerr.KindConflict {
				return duplicate
			}
		}
		return err
	}
	fmt.Fprintln(f.IO.Out, oneLine(created.HTMLURL))
	if len(reviewers) > 0 {
		if err := client.ChangePullReviewers(ctx, ref.Owner, ref.Name, created.Number, reviewers, false); err != nil {
			return fjoerr.Wrap(fjoerr.KindOf(err), err, "created %s but requesting reviewers failed", oneLine(created.HTMLURL))
		}
	}
	return nil
}

type createHead struct {
	repo                       reporef.Ref
	branch, local, remote, sha string
}

func findCreateHead(ctx context.Context, f *app.Factory, base reporef.Ref, explicit string) (createHead, error) {
	head := createHead{repo: base}
	g := f.Git()
	if explicit != "" {
		head.branch = explicit
		if owner, branch, ok := strings.Cut(explicit, ":"); ok {
			if owner == "" || branch == "" {
				return head, fjoerr.Usage("--head expects BRANCH or OWNER:BRANCH")
			}
			ref, err := f.ParseRepo(base.Host + "/" + owner + "/" + base.Name)
			if err != nil || !strings.EqualFold(ref.Owner, owner) {
				return head, fjoerr.Usage("invalid --head owner %q", owner)
			}
			head.repo, head.branch = ref, branch
		}
		return head, validBranch(ctx, g, head.branch)
	}
	local, err := g.CurrentBranch(ctx)
	if err != nil {
		return head, fjoerr.Wrap(fjoerr.KindUsage, err, "cannot determine the current branch")
	}
	head.local, head.branch = local, local
	configured, err := pushRemote(ctx, g, local)
	if err != nil {
		return head, err
	}
	remotes, err := g.Remotes(ctx)
	if err != nil {
		return head, err
	}
	cfg, err := f.Config()
	if err != nil {
		return head, err
	}
	for _, remote := range remotes {
		if configured != "" && remote.Name != configured {
			continue
		}
		parsed, err := git.ParseURL(remote.PushURL)
		if err != nil {
			continue
		}
		ref, err := reporef.FromURL(parsed, cfg)
		if err != nil || !strings.EqualFold(ref.Host, base.Host) || (configured == "" && !ref.Equal(base)) {
			continue
		}
		head.remote, head.repo = remote.Name, ref
		break
	}
	if configured != "" && head.remote == "" {
		return head, fjoerr.Usage("configured push remote %q does not point to the target Forgejo instance; specify --head", configured)
	}
	if head.remote != "" {
		tracking, err := gitSetting(ctx, g, "branch."+local+".remote")
		if err != nil {
			return head, err
		}
		if tracking == head.remote {
			trackingRef, err := gitSetting(ctx, g, "branch."+local+".merge")
			if err != nil {
				return head, err
			}
			if branch, ok := strings.CutPrefix(trackingRef, "refs/heads/"); ok {
				head.branch = branch
			}
		}
	}
	return head, validBranch(ctx, g, head.branch)
}

func gitSetting(ctx context.Context, g *git.Client, key string) (string, error) {
	value, err := g.Run(ctx, "config", "--get", key)
	var gitErr *git.Error
	if errors.As(err, &gitErr) && gitErr.ExitCode == 1 {
		return "", nil
	}
	return strings.TrimSpace(value), err
}

func pushRemote(ctx context.Context, g *git.Client, branch string) (string, error) {
	for _, key := range []string{"branch." + branch + ".pushRemote", "remote.pushDefault", "branch." + branch + ".remote"} {
		value, err := gitSetting(ctx, g, key)
		if err != nil {
			return "", err
		}
		if value != "" && value != "." {
			return value, nil
		}
	}
	return "", nil
}

func rejectDuplicate(ctx context.Context, target pullTarget, head reporef.Ref, search api.PullSearch) error {
	pulls, err := target.client.ListPulls(ctx, target.repo.Owner, target.repo.Name, search, 0)
	if err != nil {
		return err
	}
	for _, pull := range pulls {
		if pull.Head.Repo == nil || strings.EqualFold(pull.Head.Repo.FullName, head.FullName()) {
			return fjoerr.New(fjoerr.KindConflict, "an open pull request already exists: %s", oneLine(pull.HTMLURL))
		}
	}
	return nil
}

func ensureHeadOnServer(ctx context.Context, f *app.Factory, head createHead, push bool) error {
	if head.sha != "" {
		return nil
	}
	if head.remote == "" || head.local == "" {
		return fjoerr.New(fjoerr.KindNotFound, "branch %q was not found in %s", head.branch, head.repo.FullName()).
			WithHint("push the branch to Forgejo before creating the pull request")
	}
	if !push {
		if !f.IO.CanPrompt() {
			return fjoerr.New(fjoerr.KindNotFound, "branch %q has not been pushed", head.branch).
				WithHint("pass --push to publish the branch")
		}
		response, err := f.Prompter.Input(fmt.Sprintf("Push branch %s to remote %s? [Y/n]", head.branch, head.remote))
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(response)) {
		case "", "y", "yes":
		default:
			return fjoerr.New(fjoerr.KindCancelled, "push cancelled")
		}
	}
	refspec := "refs/heads/" + head.local + ":refs/heads/" + head.branch
	return f.Git().RunWithIO(ctx, io.Discard, f.IO.ErrOut, "push", "--set-upstream", "--", head.remote, refspec)
}

func createPayload(ctx context.Context, f *app.Factory, target pullTarget, opts *createOptions, head createHead, base string) (api.CreatePullOptions, error) {
	payload := api.CreatePullOptions{Base: base, Head: head.branch, Title: opts.title}
	if !head.repo.Equal(target.repo) {
		owner := head.repo.Owner
		if !strings.EqualFold(head.repo.Name, target.repo.Name) {
			owner = head.repo.FullName()
		}
		payload.Head = owner + ":" + head.branch
	}
	if opts.fill || opts.fillFirst {
		title, body, err := fillFromCommits(ctx, f, target, head, base, opts.fillFirst)
		if err != nil {
			return payload, err
		}
		if payload.Title == "" {
			payload.Title = title
		}
		payload.Body = body
	}
	if payload.Title == "" && !opts.web {
		title, err := f.Prompter.Input("Title:")
		if err != nil {
			return payload, err
		}
		payload.Title = title
	}
	if strings.TrimSpace(payload.Title) == "" && (!opts.web || opts.draft) {
		return payload, fjoerr.Usage("title cannot be empty")
	}
	if opts.draft {
		payload.Title = "WIP: " + readyTitle(payload.Title)
	}
	if opts.body.Provided() {
		body, err := readBody(ctx, f, &opts.body)
		if err != nil {
			return payload, err
		}
		payload.Body = body
	}
	var err error
	payload.Labels, err = target.client.ResolveLabelIDs(ctx, target.repo.Owner, target.repo.Name, opts.labels)
	if err != nil {
		return payload, err
	}
	payload.Assignees, err = userLogins(ctx, target.client, opts.assignees)
	if err != nil {
		return payload, err
	}
	if opts.milestone != "" {
		payload.Milestone, err = findMilestone(ctx, target, opts.milestone)
	}
	return payload, err
}

func fillFromCommits(ctx context.Context, f *app.Factory, target pullTarget, head createHead, base string, first bool) (string, string, error) {
	baseSHA, err := target.client.GetBranchSHA(ctx, target.repo.Owner, target.repo.Name, base)
	if err != nil {
		return "", "", err
	}
	if !validSHA(baseSHA) {
		return "", "", fjoerr.New(fjoerr.KindConflict, "target branch has no valid commit SHA")
	}
	headRef := head.sha
	switch {
	case validSHA(headRef):
	case headRef == "" && head.local != "":
		headRef = "refs/heads/" + head.local
	default:
		return "", "", fjoerr.New(fjoerr.KindConflict, "head branch has no valid commit SHA")
	}
	out, err := f.Git().Run(ctx, "log", "--reverse", "--format=%s%x00%b", "-z", baseSHA+".."+headRef, "--")
	if err != nil {
		return "", "", fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot read commits for --fill").
			WithHint("fetch the base and head branches before filling from commits")
	}
	parts := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", fjoerr.Usage("no commits between the base and head branches")
	}
	if first || len(parts) == 2 {
		return parts[0], strings.TrimSpace(parts[1]), nil
	}
	var body []string
	for i := 0; i+1 < len(parts); i += 2 {
		line := "- " + parts[i]
		if text := strings.TrimSpace(parts[i+1]); text != "" {
			line += "\n\n" + text
		}
		body = append(body, line)
	}
	return parts[0], strings.Join(body, "\n\n"), nil
}

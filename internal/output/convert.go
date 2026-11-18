package output

import (
	"github.com/maymeow/fjo/internal/api"
	"strings"
)

// NewIssue converts an API issue into the stable JSON schema.
func NewIssue(v *api.Issue) Issue {
	out := Issue{Number: v.Number, Title: v.Title, State: v.State, Body: v.Body, Author: User{Login: v.User.Login}, URL: v.HTMLURL, Labels: []Label{}, Assignees: []User{}, Comments: v.Comments, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, ClosedAt: v.ClosedAt}
	for _, l := range v.Labels {
		out.Labels = append(out.Labels, Label{Name: l.Name, Color: l.Color})
	}
	for _, u := range v.Assignees {
		out.Assignees = append(out.Assignees, User{Login: u.Login})
	}
	if v.Milestone != nil {
		out.Milestone = &Milestone{Title: v.Milestone.Title, Number: int(v.Milestone.ID)}
	}
	return out
}

// NewRepository converts a Forgejo repository into the normalized JSON model.
func NewRepository(r *api.Repository) Repository {
	out := Repository{
		Name:             r.Name,
		NameWithOwner:    r.FullName,
		Description:      r.Description,
		IsPrivate:        r.Private,
		IsInternal:       r.Internal,
		IsFork:           r.Fork,
		IsMirror:         r.Mirror,
		IsTemplate:       r.Template,
		IsArchived:       r.Archived,
		IsEmpty:          r.Empty,
		DefaultBranch:    r.DefaultBranch,
		PrimaryLanguage:  r.Language,
		Topics:           append([]string{}, r.Topics...),
		URL:              r.HTMLURL,
		CloneURL:         r.CloneURL,
		SSHURL:           r.SSHURL,
		WebsiteURL:       r.Website,
		StargazerCount:   r.Stars,
		ForkCount:        r.Forks,
		OpenIssueCount:   r.OpenIssues,
		OpenPullRequests: r.OpenPulls,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
	if r.Parent != nil {
		out.Parent = &RepositoryID{NameWithOwner: r.Parent.FullName}
	}
	return out
}

// NewPullRequest converts a Forgejo pull request into the stable JSON schema.
func NewPullRequest(p *api.PullRequest) PullRequest {
	out := PullRequest{
		Number: p.Number, Title: p.Title, Body: p.Body, State: p.State,
		Author: User{Login: p.User.Login}, URL: p.HTMLURL, IsDraft: p.IsDraft(),
		BaseRefName: p.Base.Ref, HeadRefName: p.Head.Ref, Mergeable: "UNKNOWN",
		Labels: []Label{}, Assignees: []User{}, Reviewers: []User{},
		Comments: p.Comments, Additions: p.Additions, Deletions: p.Deletions, ChangedFiles: p.ChangedFiles,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, ClosedAt: p.ClosedAt, MergedAt: p.MergedAt,
	}
	if p.IsMerged() {
		out.State = "merged"
	}
	if p.Head.Repo != nil {
		out.HeadRepository = p.Head.Repo.FullName
	}
	if p.Head.Repo != nil && p.Base.Repo != nil {
		out.IsCrossRepo = !strings.EqualFold(p.Head.Repo.FullName, p.Base.Repo.FullName)
	} else if head, base := p.Head.Owner(), p.Base.Owner(); head != "" && base != "" {
		out.IsCrossRepo = !strings.EqualFold(head, base)
	}
	if p.Mergeable != nil {
		out.Mergeable = "CONFLICTING"
		if *p.Mergeable {
			out.Mergeable = "MERGEABLE"
		}
	}
	for _, label := range p.Labels {
		out.Labels = append(out.Labels, Label{Name: label.Name, Color: label.Color})
	}
	for _, user := range p.Assignees {
		out.Assignees = append(out.Assignees, User{Login: user.Login})
	}
	if len(p.Assignees) == 0 && p.Assignee != nil {
		out.Assignees = append(out.Assignees, User{Login: p.Assignee.Login})
	}
	for _, user := range p.Reviewers {
		out.Reviewers = append(out.Reviewers, User{Login: user.Login})
	}
	if p.Milestone != nil {
		out.Milestone = &Milestone{Title: p.Milestone.Title, Number: int(p.Milestone.ID)}
	}
	return out
}

// NewCheck converts a Forgejo commit status into a check and its gh-style bucket.
func NewCheck(status api.CommitStatus) Check {
	out := Check{
		Name: status.Context, State: status.State, Description: status.Description,
		URL: status.TargetURL, StartedAt: status.CreatedAt,
	}
	switch status.State {
	case "success":
		out.Bucket = "pass"
	case "failure", "error", "warning":
		out.Bucket = "fail"
	case "skipped":
		out.Bucket = "skipping"
	default:
		out.Bucket = "pending"
	}
	if out.Bucket != "pending" {
		out.CompletedAt = status.UpdatedAt
	}
	return out
}

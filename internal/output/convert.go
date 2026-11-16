package output

import (
	"github.com/maymeow/fjo/internal/api"
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

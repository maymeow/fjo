package output

import (
	"github.com/maymeow/fjo/internal/api"
)

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

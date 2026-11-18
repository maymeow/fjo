package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/maymeow/fjo/internal/fjoerr"
)

var (
	epGetUser        = endpoint("GET", "/users/{username}")
	epGetRepo        = endpoint("GET", "/repos/{owner}/{repo}")
	epSearchRepo     = endpoint("GET", "/repos/search")
	epCreateUserRepo = endpoint("POST", "/user/repos")
	epCreateOrgRepo  = endpoint("POST", "/orgs/{org}/repos")
	epEditRepo       = endpoint("PATCH", "/repos/{owner}/{repo}")
	epDeleteRepo     = endpoint("DELETE", "/repos/{owner}/{repo}")
	epForkRepo       = endpoint("POST", "/repos/{owner}/{repo}/forks")
	epSyncFork       = endpoint("POST", "/repos/{owner}/{repo}/sync_fork")
	epSyncForkBranch = endpoint("POST", "/repos/{owner}/{repo}/sync_fork/{branch}")
)

// CreateRepoOptions is the body of POST /user/repos and /orgs/{org}/repos
// (CreateRepoOption in the Forgejo API). Private is a pointer so --public can
// explicitly request false instead of falling back to the server default.
type CreateRepoOptions struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Private       *bool  `json:"private,omitempty"`
	AutoInit      bool   `json:"auto_init,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Gitignores    string `json:"gitignores,omitempty"`
	License       string `json:"license,omitempty"`
	Readme        string `json:"readme,omitempty"`
}

// EditRepoOptions is the body of PATCH /repos/{owner}/{repo}
// (EditRepoOption). Pointer fields distinguish "unset" from "set to zero".
type EditRepoOptions struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Website       *string `json:"website,omitempty"`
	Private       *bool   `json:"private,omitempty"`
	Archived      *bool   `json:"archived,omitempty"`
	DefaultBranch *string `json:"default_branch,omitempty"`
}

// ForkRepoOptions is the body of POST /repos/{owner}/{repo}/forks
// (CreateForkOption).
type ForkRepoOptions struct {
	Name         string `json:"name,omitempty"`
	Organization string `json:"organization,omitempty"`
}

// Repository is the subset of Forgejo's Repository model fjo uses.
type Repository struct {
	ID            int64       `json:"id"`
	Name          string      `json:"name"`
	FullName      string      `json:"full_name"`
	Owner         User        `json:"owner"`
	Description   string      `json:"description"`
	Website       string      `json:"website"`
	Private       bool        `json:"private"`
	Internal      bool        `json:"internal"`
	Fork          bool        `json:"fork"`
	Mirror        bool        `json:"mirror"`
	Template      bool        `json:"template"`
	Archived      bool        `json:"archived"`
	Empty         bool        `json:"empty"`
	Parent        *Repository `json:"parent"`
	HTMLURL       string      `json:"html_url"`
	CloneURL      string      `json:"clone_url"`
	SSHURL        string      `json:"ssh_url"`
	DefaultBranch string      `json:"default_branch"`
	Language      string      `json:"language"`
	Topics        []string    `json:"topics"`
	Stars         int         `json:"stars_count"`
	Forks         int         `json:"forks_count"`
	Watchers      int         `json:"watchers_count"`
	OpenIssues    int         `json:"open_issues_count"`
	OpenPulls     int         `json:"open_pr_counter"`
	Releases      int         `json:"release_counter"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// GetUser returns a user or organization by name (GET /users/{username}).
func (c *Client) GetUser(ctx context.Context, name string) (*User, error) {
	var u User
	if err := c.JSON(ctx, http.MethodGet, epGetUser.Expand(name), nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetRepository returns one repository.
func (c *Client) GetRepository(ctx context.Context, owner, name string) (*Repository, error) {
	var r Repository
	if err := c.JSON(ctx, http.MethodGet, epGetRepo.Expand(owner, name), nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateRepository creates a repository for the authenticated user (owner
// empty) or an organization (non-empty owner).
func (c *Client) CreateRepository(ctx context.Context, owner string, opt CreateRepoOptions) (*Repository, error) {
	path := epCreateUserRepo.Expand()
	if owner != "" {
		path = epCreateOrgRepo.Expand(owner)
	}
	var r Repository
	if err := c.JSON(ctx, http.MethodPost, path, opt, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// EditRepository updates repository properties. Only fields set in opt change.
func (c *Client) EditRepository(ctx context.Context, owner, name string, opt EditRepoOptions) (*Repository, error) {
	var r Repository
	if err := c.JSON(ctx, http.MethodPatch, epEditRepo.Expand(owner, name), opt, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteRepository deletes a repository.
func (c *Client) DeleteRepository(ctx context.Context, owner, name string) error {
	err := c.JSON(ctx, http.MethodDelete, epDeleteRepo.Expand(owner, name), nil, nil)
	return err
}

// ForkRepository forks a repository into the authenticated user's account or
// the given organization.
func (c *Client) ForkRepository(ctx context.Context, owner, name string, opt ForkRepoOptions) (*Repository, error) {
	var r Repository
	if err := c.JSON(ctx, http.MethodPost, epForkRepo.Expand(owner, name), opt, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SyncFork syncs a fork's default branch (branch empty) or one branch with its
// upstream base branch.
func (c *Client) SyncFork(ctx context.Context, owner, name, branch string) error {
	path := epSyncFork.Expand(owner, name)
	if branch != "" {
		path = epSyncForkBranch.Expand(owner, name, branch)
	}
	err := c.JSON(ctx, http.MethodPost, path, nil, nil)
	return err
}

// RepoSearch filters GET /repos/search. Nil booleans mean "any".
type RepoSearch struct {
	OwnerID  int64 // only repositories owned by this user/org (exclusive)
	Private  *bool // is_private
	Archived *bool
	Mode     string // "", "fork", "source", "mirror", "collaborative"
	Sort     string // e.g. "updated"
	Order    string // "asc" | "desc"
}

// SearchRepositories returns up to limit repositories (all when limit <= 0)
// and the server's total count (-1 if unknown).
func (c *Client) SearchRepositories(ctx context.Context, s RepoSearch, limit int) ([]Repository, int64, error) {
	q := url.Values{}
	if s.OwnerID > 0 {
		q.Set("uid", strconv.FormatInt(s.OwnerID, 10))
		q.Set("exclusive", "true")
	}
	if s.Private != nil {
		q.Set("is_private", strconv.FormatBool(*s.Private))
	}
	if s.Archived != nil {
		q.Set("archived", strconv.FormatBool(*s.Archived))
	}
	if s.Mode != "" {
		q.Set("mode", s.Mode)
	}
	if s.Sort != "" {
		q.Set("sort", s.Sort)
		if s.Order != "" {
			q.Set("order", s.Order)
		}
	}
	size := 50
	if limit > 0 && limit < size {
		size = limit
	}
	q.Set("limit", strconv.Itoa(size))

	var (
		out   []Repository
		total int64 = -1
	)
	err := c.Pages(ctx, epSearchRepo.Expand()+"?"+q.Encode(), nil, 0, func(resp *http.Response) (bool, error) {
		if total < 0 {
			total = ParsePage(resp.Header).Total
		}
		var page struct {
			OK   bool         `json:"ok"`
			Data []Repository `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return true, fjoerr.Wrap(fjoerr.KindGeneral, err, "invalid response from %s", SanitizeURL(resp.Request.URL))
		}
		out = append(out, page.Data...)
		if limit > 0 && len(out) >= limit {
			out = out[:limit]
			return true, nil
		}
		return len(page.Data) == 0, nil
	})
	return out, total, err
}

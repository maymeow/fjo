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
	epGetUser    = endpoint("GET", "/users/{username}")
	epGetRepo    = endpoint("GET", "/repos/{owner}/{repo}")
	epSearchRepo = endpoint("GET", "/repos/search")
)

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
	if _, err := c.JSON(ctx, http.MethodGet, epGetUser.Expand(name), nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetRepository returns one repository.
func (c *Client) GetRepository(ctx context.Context, owner, name string) (*Repository, error) {
	var r Repository
	if _, err := c.JSON(ctx, http.MethodGet, epGetRepo.Expand(owner, name), nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
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

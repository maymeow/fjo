package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type PullBranch struct {
	Label string      `json:"label"`
	Ref   string      `json:"ref"`
	SHA   string      `json:"sha"`
	Repo  *Repository `json:"repo"`
}

func (b PullBranch) Owner() string {
	if b.Repo != nil {
		if owner, _, ok := strings.Cut(b.Repo.FullName, "/"); ok {
			return owner
		}
	}
	owner, _, ok := strings.Cut(b.Label, ":")
	if !ok {
		return ""
	}
	return owner
}

type PullRequest struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	State        string     `json:"state"`
	Body         string     `json:"body"`
	User         User       `json:"user"`
	HTMLURL      string     `json:"html_url"`
	Labels       []Label    `json:"labels"`
	Assignee     *User      `json:"assignee"`
	Assignees    []User     `json:"assignees"`
	Reviewers    []User     `json:"requested_reviewers"`
	Milestone    *Milestone `json:"milestone"`
	Base         PullBranch `json:"base"`
	Head         PullBranch `json:"head"`
	Comments     int        `json:"comments"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changed_files"`
	Draft        bool       `json:"draft"`
	Merged       bool       `json:"merged"`
	Mergeable    *bool      `json:"mergeable"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ClosedAt     *time.Time `json:"closed_at"`
	MergedAt     *time.Time `json:"merged_at"`
}

func (p PullRequest) IsMerged() bool { return p.Merged || (p.MergedAt != nil && !p.MergedAt.IsZero()) }

func (p PullRequest) IsDraft() bool {
	title := strings.ToUpper(p.Title)
	return p.Draft || strings.HasPrefix(title, "WIP:") || strings.HasPrefix(title, "[WIP]")
}

func (p PullRequest) AssignedTo(login string) bool {
	for _, user := range p.Assignees {
		if strings.EqualFold(user.Login, login) {
			return true
		}
	}
	return p.Assignee != nil && strings.EqualFold(p.Assignee.Login, login)
}

type PullSearch struct {
	State, Author, Assignee, Base, Head, HeadOwner string
	Labels                                         []int64
	Draft                                          bool
}

func (c *Client) ListPulls(ctx context.Context, owner, repo string, s PullSearch, limit int) ([]PullRequest, error) {
	q := url.Values{}
	state := s.State
	if state == "merged" {
		state = "closed"
	}
	if state != "" {
		q.Set("state", state)
	}
	if s.Author != "" {
		q.Set("poster", s.Author)
	}
	for _, label := range s.Labels {
		q.Add("labels", strconv.FormatInt(label, 10))
	}
	if s.Base != "" || s.Head != "" {
		version, err := c.ServerVersion(ctx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
		if n, parseErr := strconv.Atoi(major); err == nil && parseErr == nil && n >= 17 {
			if s.Base != "" {
				q.Set("base", s.Base)
			}
			if s.Head != "" {
				q.Set("head", s.Head)
			}
		}
	}
	path := epListPulls.Expand(owner, repo)
	if s.State != "merged" && s.State != "closed" && !s.Draft && s.Assignee == "" && s.Base == "" && s.Head == "" && s.HeadOwner == "" {
		return List[PullRequest](ctx, c, path, q, limit)
	}
	return listMatching(ctx, c, path, q, limit, func(p PullRequest) bool {
		return (s.State != "merged" || p.IsMerged()) &&
			(s.State != "closed" || !p.IsMerged()) &&
			(!s.Draft || p.IsDraft()) &&
			(s.Assignee == "" || p.AssignedTo(s.Assignee)) &&
			(s.Base == "" || p.Base.Ref == s.Base) &&
			(s.Head == "" || p.Head.Ref == s.Head) &&
			(s.HeadOwner == "" || strings.EqualFold(p.Head.Owner(), s.HeadOwner))
	})
}

func (c *Client) GetPull(ctx context.Context, owner, repo string, number int) (*PullRequest, error) {
	var pr PullRequest
	err := c.JSON(ctx, http.MethodGet, epGetPull.Expand(owner, repo, strconv.Itoa(number)), nil, &pr)
	return &pr, err
}

// PullDiff returns the raw diff or patch. The caller closes the stream.
func (c *Client) PullDiff(ctx context.Context, owner, repo string, number int, patch bool) (io.ReadCloser, error) {
	ext := "diff"
	if patch {
		ext = "patch"
	}
	req, err := c.NewRequest(ctx, http.MethodGet, epPullDiff.Expand(owner, repo, strconv.Itoa(number), ext)+"?binary=true", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if err := CheckResponse(resp); err != nil {
		return nil, err
	}
	return resp.Body, nil
}

type PullFile struct {
	Name string `json:"filename"`
}

func (c *Client) ListPullFiles(ctx context.Context, owner, repo string, number int) ([]PullFile, error) {
	return List[PullFile](ctx, c, epPullFiles.Expand(owner, repo, strconv.Itoa(number)), nil, 0)
}

type CommitStatus struct {
	ID          int64      `json:"id"`
	Context     string     `json:"context"`
	State       string     `json:"status"`
	Description string     `json:"description"`
	TargetURL   string     `json:"target_url"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

func (c *Client) ListCommitStatuses(ctx context.Context, owner, repo, sha string) ([]CommitStatus, error) {
	return List[CommitStatus](ctx, c, epCommitStatuses.Expand(owner, repo, sha), nil, 0)
}

package api

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maymeow/fjo/internal/fjoerr"
)

type Label struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}
type Milestone struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}
type Issue struct {
	ID          int64      `json:"id"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	User        User       `json:"user"`
	HTMLURL     string     `json:"html_url"`
	Labels      []Label    `json:"labels"`
	Assignees   []User     `json:"assignees"`
	Milestone   *Milestone `json:"milestone"`
	Comments    int        `json:"comments"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	PullRequest *struct{}  `json:"pull_request"`
}
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type IssueSearch struct {
	State, Author, Assignee, MentionedBy, Milestone, Search string
	Labels                                                  []string
}
type CreateIssueOptions struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []int64  `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Milestone int64    `json:"milestone,omitempty"`
}
type EditIssueOptions struct {
	Title     *string   `json:"title,omitempty"`
	Body      *string   `json:"body,omitempty"`
	State     *string   `json:"state,omitempty"`
	Assignees *[]string `json:"assignees,omitempty"`
	Milestone *int64    `json:"milestone,omitempty"`
}

func (c *Client) ListIssues(ctx context.Context, owner, repo string, s IssueSearch, limit int) ([]Issue, error) {
	q := url.Values{"type": {"issues"}}
	for k, v := range map[string]string{"state": s.State, "created_by": s.Author, "assigned_by": s.Assignee, "mentioned_by": s.MentionedBy, "milestones": s.Milestone, "q": s.Search, "labels": strings.Join(s.Labels, ",")} {
		if v != "" {
			q.Set(k, v)
		}
	}
	return List[Issue](ctx, c, epListIssues.Expand(owner, repo), q, limit)
}
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	var v Issue
	err := c.JSON(ctx, "GET", epGetIssue.Expand(owner, repo, strconv.Itoa(number)), nil, &v)
	return &v, err
}
func (c *Client) CreateIssue(ctx context.Context, owner, repo string, opt CreateIssueOptions) (*Issue, error) {
	var v Issue
	err := c.JSON(ctx, "POST", epCreateIssue.Expand(owner, repo), opt, &v)
	return &v, err
}
func (c *Client) EditIssue(ctx context.Context, owner, repo string, number int, opt EditIssueOptions) (*Issue, error) {
	var v Issue
	err := c.JSON(ctx, "PATCH", epEditIssue.Expand(owner, repo, strconv.Itoa(number)), opt, &v)
	return &v, err
}
func (c *Client) DeleteIssue(ctx context.Context, owner, repo string, number int) error {
	err := c.JSON(ctx, "DELETE", epDeleteIssue.Expand(owner, repo, strconv.Itoa(number)), nil, nil)
	return err
}
func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]Comment, error) {
	return List[Comment](ctx, c, epListIssueComments.Expand(owner, repo, strconv.Itoa(number)), nil, 0)
}
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) (*Comment, error) {
	var v Comment
	err := c.JSON(ctx, "POST", epCreateIssueComment.Expand(owner, repo, strconv.Itoa(number)), map[string]string{"body": body}, &v)
	return &v, err
}
func (c *Client) ListLabels(ctx context.Context, owner, repo string) ([]Label, error) {
	return List[Label](ctx, c, epListLabels.Expand(owner, repo), nil, 0)
}
func (c *Client) ListMilestones(ctx context.Context, owner, repo string) ([]Milestone, error) {
	return List[Milestone](ctx, c, epListMilestones.Expand(owner, repo), url.Values{"state": {"all"}}, 0)
}
func (c *Client) AddIssueLabels(ctx context.Context, owner, repo string, number int, ids []int64) error {
	err := c.JSON(ctx, "POST", epAddIssueLabels.Expand(owner, repo, strconv.Itoa(number)), map[string]any{"labels": ids}, nil)
	return err
}
func (c *Client) RemoveIssueLabel(ctx context.Context, owner, repo string, number int, id int64) error {
	err := c.JSON(ctx, "DELETE", epRemoveIssueLabel.Expand(owner, repo, strconv.Itoa(number), strconv.FormatInt(id, 10)), nil, nil)
	return err
}
func (c *Client) PinIssue(ctx context.Context, owner, repo string, number int, pin bool) error {
	ep := epPinIssue
	if !pin {
		ep = epUnpinIssue
	}
	err := c.JSON(ctx, ep.Method, ep.Expand(owner, repo, strconv.Itoa(number)), nil, nil)
	return err
}

// ResolveLabelIDs translates repository label names to Forgejo IDs.
func (c *Client) ResolveLabelIDs(ctx context.Context, owner, repo string, names []string) ([]int64, error) {
	ids := []int64{}
	if len(names) == 0 {
		return ids, nil
	}
	labels, err := c.ListLabels(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		found := false
		for _, l := range labels {
			if l.Name == name {
				ids = append(ids, l.ID)
				found = true
				break
			}
		}
		if !found {
			return nil, fjoerr.New(fjoerr.KindNotFound, "label %q not found in %s/%s", name, owner, repo)
		}
	}
	return ids, nil
}

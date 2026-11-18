package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/maymeow/fjo/internal/fjoerr"
)

var (
	epCreatePull    = endpoint(http.MethodPost, "/repos/{owner}/{repo}/pulls")
	epEditPull      = endpoint(http.MethodPatch, "/repos/{owner}/{repo}/pulls/{index}")
	epMergePull     = endpoint(http.MethodPost, "/repos/{owner}/{repo}/pulls/{index}/merge")
	epRequestReview = endpoint(http.MethodPost, "/repos/{owner}/{repo}/pulls/{index}/requested_reviewers")
	epRemoveReview  = endpoint(http.MethodDelete, "/repos/{owner}/{repo}/pulls/{index}/requested_reviewers")
	epGetBranch     = endpoint(http.MethodGet, "/repos/{owner}/{repo}/branches/{branch}")
	epDeleteBranch  = endpoint(http.MethodDelete, "/repos/{owner}/{repo}/branches/{branch}")

	epListPullReviews  = endpoint(http.MethodGet, "/repos/{owner}/{repo}/pulls/{index}/reviews")
	epCreatePullReview = endpoint(http.MethodPost, "/repos/{owner}/{repo}/pulls/{index}/reviews")
)

// Review events accepted by CreatePullReview.
const (
	ReviewApprove        = "APPROVED"
	ReviewRequestChanges = "REQUEST_CHANGES"
	ReviewComment        = "COMMENT"
)

// CreatePullReviewOptions submits a review for the given head commit.
type CreatePullReviewOptions struct {
	Event    string `json:"event"`
	Body     string `json:"body,omitempty"`
	CommitID string `json:"commit_id,omitempty"`
}

// PullReview is a submitted pull request review.
type PullReview struct {
	ID      int64  `json:"id"`
	State   string `json:"state"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

// CreatePullOptions describes a new Forgejo pull request.
type CreatePullOptions struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Base      string   `json:"base"`
	Head      string   `json:"head"`
	Labels    []int64  `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Milestone int64    `json:"milestone,omitempty"`
}

// EditPullOptions distinguishes absent fields from explicit empty values.
type EditPullOptions struct {
	Title     *string   `json:"title,omitempty"`
	Body      *string   `json:"body,omitempty"`
	Base      *string   `json:"base,omitempty"`
	State     *string   `json:"state,omitempty"`
	Assignees *[]string `json:"assignees,omitempty"`
	Milestone *int64    `json:"milestone,omitempty"`
}

// MergePullOptions describes a merge or a request to merge after checks pass.
type MergePullOptions struct {
	Method       string `json:"Do"`
	Subject      string `json:"MergeTitleField,omitempty"`
	Body         string `json:"MergeMessageField,omitempty"`
	HeadSHA      string `json:"head_commit_id,omitempty"`
	Auto         bool   `json:"merge_when_checks_succeed,omitempty"`
	DeleteBranch bool   `json:"delete_branch_after_merge"`
}

// CreatePull creates a pull request by branch name.
func (c *Client) CreatePull(ctx context.Context, owner, repo string, opt CreatePullOptions) (*PullRequest, error) {
	var pr PullRequest
	err := c.JSON(ctx, epCreatePull.Method, epCreatePull.Expand(owner, repo), opt, &pr)
	return &pr, err
}

// EditPull changes fields on an existing pull request.
func (c *Client) EditPull(ctx context.Context, owner, repo string, index int, opt EditPullOptions) error {
	return c.JSON(ctx, epEditPull.Method, epEditPull.Expand(owner, repo, strconv.Itoa(index)), opt, nil)
}

// MergePull merges a pull request using the requested method.
func (c *Client) MergePull(ctx context.Context, owner, repo string, index int, opt MergePullOptions) error {
	return c.JSON(ctx, epMergePull.Method, epMergePull.Expand(owner, repo, strconv.Itoa(index)), opt, nil)
}

// ChangePullReviewers adds or removes requests for the supplied users.
func (c *Client) ChangePullReviewers(ctx context.Context, owner, repo string, index int, logins []string, remove bool) error {
	ep := epRequestReview
	if remove {
		ep = epRemoveReview
	}
	return c.JSON(ctx, ep.Method, ep.Expand(owner, repo, strconv.Itoa(index)), map[string][]string{"reviewers": logins}, nil)
}

// GetBranchSHA returns the commit currently at the tip of a repository branch.
func (c *Client) GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	var result struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	err := c.JSON(ctx, epGetBranch.Method, epGetBranch.Expand(owner, repo, branch), nil, &result)
	return result.Commit.ID, err
}

// DeleteBranch removes a branch from the specified repository.
func (c *Client) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	return c.JSON(ctx, epDeleteBranch.Method, epDeleteBranch.Expand(owner, repo, branch), nil, nil)
}

// SupportsPullReviews probes the review list of a pull request. The caller
// must already know that the pull request exists: a 404 then means the server
// has no review API (safe probing, IMPLEMENTATION §5.5 and BACKLOG B-9).
func (c *Client) SupportsPullReviews(ctx context.Context, owner, repo string, index int) (bool, error) {
	_, err := List[PullReview](ctx, c, epListPullReviews.Expand(owner, repo, strconv.Itoa(index)), nil, 1)
	switch {
	case err == nil:
		return true, nil
	case fjoerr.KindOf(err) == fjoerr.KindNotFound:
		return false, nil
	default:
		return false, err
	}
}

// CreatePullReview submits an approval, a change request or a comment review.
func (c *Client) CreatePullReview(ctx context.Context, owner, repo string, index int, opt CreatePullReviewOptions) (*PullReview, error) {
	var review PullReview
	err := c.JSON(ctx, epCreatePullReview.Method, epCreatePullReview.Expand(owner, repo, strconv.Itoa(index)), opt, &review)
	return &review, err
}

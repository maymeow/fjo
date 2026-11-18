package api

import (
	"context"
	"strconv"
)

// CreateRelease creates a release in Forgejo.
func (c *Client) CreateRelease(ctx context.Context, owner, repo string, options CreateReleaseOptions) (*Release, error) {
	var result Release
	err := c.JSON(ctx, epCreateRelease.Method, epCreateRelease.Expand(owner, repo), options, &result)
	return &result, err
}

// EditRelease applies the supplied fields to the release identified by ID.
func (c *Client) EditRelease(ctx context.Context, owner, repo string, id int64, options EditReleaseOptions) (*Release, error) {
	var result Release
	err := c.JSON(ctx, epEditRelease.Method, epEditRelease.Expand(owner, repo, strconv.FormatInt(id, 10)), options, &result)
	return &result, err
}

// DeleteRelease removes a release while leaving its Git tag in place.
func (c *Client) DeleteRelease(ctx context.Context, owner, repo string, id int64) error {
	return c.JSON(ctx, epDeleteRelease.Method, epDeleteRelease.Expand(owner, repo, strconv.FormatInt(id, 10)), nil, nil)
}

// VerifyTag checks that a Git tag exists on Forgejo.
func (c *Client) VerifyTag(ctx context.Context, owner, repo, tag string) error {
	return c.JSON(ctx, epGetTag.Method, epGetTag.Expand(owner, repo, tag), nil, nil)
}

// DeleteTag removes a Git tag from Forgejo.
func (c *Client) DeleteTag(ctx context.Context, owner, repo, tag string) error {
	return c.JSON(ctx, epDeleteTag.Method, epDeleteTag.Expand(owner, repo, tag), nil, nil)
}

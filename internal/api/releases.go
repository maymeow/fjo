package api

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// Release is the Forgejo release response and its embedded attachments.
type Release struct {
	ID               int64        `json:"id"`
	TagName          string       `json:"tag_name"`
	Name             string       `json:"name"`
	Body             string       `json:"body"`
	Draft            bool         `json:"draft"`
	Prerelease       bool         `json:"prerelease"`
	HideArchiveLinks bool         `json:"hide_archive_links"`
	CreatedAt        time.Time    `json:"created_at"`
	PublishedAt      *time.Time   `json:"published_at"`
	HTMLURL          string       `json:"html_url"`
	URL              string       `json:"url"`
	TarballURL       string       `json:"tarball_url"`
	ZipballURL       string       `json:"zipball_url"`
	TargetCommitish  string       `json:"target_commitish"`
	Author           User         `json:"author"`
	Assets           []Attachment `json:"assets"`
}

// Attachment is the Forgejo metadata for a release asset.
type Attachment struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Size          int64     `json:"size"`
	DownloadURL   string    `json:"browser_download_url"`
	DownloadCount int64     `json:"download_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// CreateReleaseOptions is the Forgejo release creation payload.
type CreateReleaseOptions struct {
	TagName          string `json:"tag_name"`
	Name             string `json:"name,omitempty"`
	Body             string `json:"body,omitempty"`
	TargetCommitish  string `json:"target_commitish,omitempty"`
	Draft            bool   `json:"draft,omitempty"`
	Prerelease       bool   `json:"prerelease,omitempty"`
	HideArchiveLinks bool   `json:"hide_archive_links,omitempty"`
}

// EditReleaseOptions distinguishes absent fields from explicit empty values.
type EditReleaseOptions struct {
	TagName          *string `json:"tag_name,omitempty"`
	Name             *string `json:"name,omitempty"`
	Body             *string `json:"body,omitempty"`
	TargetCommitish  *string `json:"target_commitish,omitempty"`
	Draft            *bool   `json:"draft,omitempty"`
	Prerelease       *bool   `json:"prerelease,omitempty"`
	HideArchiveLinks *bool   `json:"hide_archive_links,omitempty"`
}

// ReleaseListOptions controls which releases are included in a listing.
type ReleaseListOptions struct {
	ExcludeDrafts      bool
	ExcludePrereleases bool
}

// ListReleases returns matching releases in Forgejo's pagination order.
func (c *Client) ListReleases(ctx context.Context, owner, repo string, options ReleaseListOptions, limit int) ([]Release, error) {
	path := epListReleases.Expand(owner, repo)
	if !options.ExcludeDrafts && !options.ExcludePrereleases {
		return List[Release](ctx, c, path, nil, limit)
	}
	query := url.Values{}
	if options.ExcludeDrafts {
		query.Set("draft", "false")
	}
	if options.ExcludePrereleases {
		query.Set("pre-release", "false")
	}
	return listMatching(ctx, c, path, query, limit, func(r Release) bool {
		return (!options.ExcludeDrafts || !r.Draft) && (!options.ExcludePrereleases || !r.Prerelease)
	})
}

// LatestRelease returns the latest stable published release.
func (c *Client) LatestRelease(ctx context.Context, owner, repo string) (*Release, error) {
	var release Release
	if err := c.JSON(ctx, epLatestRelease.Method, epLatestRelease.Expand(owner, repo), nil, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

// ReleaseByTag resolves a release by exact tag, including visible drafts.
func (c *Client) ReleaseByTag(ctx context.Context, owner, repo, tag string) (*Release, error) {
	if strings.TrimSpace(tag) == "" || tag == "." || tag == ".." {
		return nil, fjoerr.Usage("invalid release tag %q", tag)
	}
	var release Release
	lookupErr := c.JSON(ctx, epReleaseByTag.Method, epReleaseByTag.Expand(owner, repo, tag), nil, &release)
	if lookupErr == nil {
		return &release, nil
	}
	if fjoerr.KindOf(lookupErr) != fjoerr.KindNotFound {
		return nil, lookupErr
	}
	query := url.Values{"draft": {"true"}}
	releases, err := listMatching(ctx, c, epListReleases.Expand(owner, repo), query, 1, func(r Release) bool {
		return r.Draft && r.TagName == tag
	})
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, lookupErr
	}
	return &releases[0], nil
}

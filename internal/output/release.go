package output

import (
	"time"

	"github.com/maymeow/fjo/internal/api"
)

// Release is the normalized schema for release list and view JSON.
type Release struct {
	ID              int64          `json:"id"`
	TagName         string         `json:"tagName"`
	Name            string         `json:"name"`
	Body            string         `json:"body"`
	IsDraft         bool           `json:"isDraft"`
	IsPrerelease    bool           `json:"isPrerelease"`
	CreatedAt       time.Time      `json:"createdAt"`
	PublishedAt     *time.Time     `json:"publishedAt"`
	URL             string         `json:"url"`
	TarballURL      string         `json:"tarballUrl"`
	ZipballURL      string         `json:"zipballUrl"`
	TargetCommitish string         `json:"targetCommitish"`
	Author          User           `json:"author"`
	Assets          []ReleaseAsset `json:"assets"`
}

// ReleaseAsset is the normalized JSON representation of an attachment.
type ReleaseAsset struct {
	Name          string    `json:"name"`
	Size          int64     `json:"size"`
	URL           string    `json:"url"`
	DownloadCount int64     `json:"downloadCount"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ReleaseFields lists the selectable fields for release JSON output.
var ReleaseFields = []string{
	"assets", "author", "body", "createdAt", "id", "isDraft", "isPrerelease", "name",
	"publishedAt", "tagName", "tarballUrl", "targetCommitish", "url", "zipballUrl",
}

// NewRelease converts a Forgejo release into its stable JSON schema.
func NewRelease(r api.Release) Release {
	result := Release{
		ID: r.ID, TagName: r.TagName, Name: r.Name, Body: r.Body, IsDraft: r.Draft, IsPrerelease: r.Prerelease,
		CreatedAt: r.CreatedAt, URL: r.HTMLURL, TarballURL: r.TarballURL, ZipballURL: r.ZipballURL,
		TargetCommitish: r.TargetCommitish, Author: User{Login: r.Author.Login},
		Assets: make([]ReleaseAsset, 0, len(r.Assets)),
	}
	if !r.Draft && r.PublishedAt != nil && !r.PublishedAt.IsZero() && r.PublishedAt.Unix() > 0 {
		result.PublishedAt = r.PublishedAt
	}
	for _, asset := range r.Assets {
		result.Assets = append(result.Assets, ReleaseAsset{
			Name: asset.Name, Size: asset.Size, URL: asset.DownloadURL, DownloadCount: asset.DownloadCount, CreatedAt: asset.CreatedAt,
		})
	}
	return result
}

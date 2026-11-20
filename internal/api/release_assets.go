package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// AttachmentSettings are the instance-wide attachment settings Forgejo
// exposes through GET /settings/attachment.
type AttachmentSettings struct {
	Enabled      bool   `json:"enabled"`
	AllowedTypes string `json:"allowed_types"`
	MaxSize      int64  `json:"max_size"`
	MaxFiles     int64  `json:"max_files"`
}

// AttachmentSettings returns the instance's attachment settings.
func (c *Client) AttachmentSettings(ctx context.Context) (*AttachmentSettings, error) {
	var settings AttachmentSettings
	if err := c.JSON(ctx, epAttachmentSettings.Method, epAttachmentSettings.Expand(), nil, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// UploadReleaseAsset streams exactly size bytes from body as a new asset
// called name. The body is sent as application/octet-stream, is never
// buffered or retried, and is not closed by the client.
func (c *Client) UploadReleaseAsset(ctx context.Context, owner, repo string, releaseID int64, name string, body io.Reader, size int64) (result *Attachment, err error) {
	path := epUploadReleaseAsset.Expand(owner, repo, strconv.FormatInt(releaseID, 10)) + "?" + url.Values{"name": {name}}.Encode()
	payload := io.NopCloser(body)
	if size == 0 {
		payload = http.NoBody
	}
	req, err := c.NewRequest(ctx, epUploadReleaseAsset.Method, path, payload)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if err := CheckResponse(resp); err != nil {
		return nil, err
	}
	defer func() { err = fjoerr.JoinCleanup(err, resp.Body.Close()) }()
	var asset Attachment
	if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindGeneral, err, "invalid response from %s %s", req.Method, SanitizeURL(req.URL))
	}
	return &asset, nil
}

// RenameReleaseAsset changes the name of a release asset.
func (c *Client) RenameReleaseAsset(ctx context.Context, owner, repo string, releaseID, assetID int64, name string) (*Attachment, error) {
	var asset Attachment
	path := epEditReleaseAsset.Expand(owner, repo, strconv.FormatInt(releaseID, 10), strconv.FormatInt(assetID, 10))
	if err := c.JSON(ctx, epEditReleaseAsset.Method, path, map[string]string{"name": name}, &asset); err != nil {
		return nil, err
	}
	return &asset, nil
}

// DeleteReleaseAsset removes a release asset.
func (c *Client) DeleteReleaseAsset(ctx context.Context, owner, repo string, releaseID, assetID int64) error {
	path := epDeleteReleaseAsset.Expand(owner, repo, strconv.FormatInt(releaseID, 10), strconv.FormatInt(assetID, 10))
	return c.JSON(ctx, epDeleteReleaseAsset.Method, path, nil, nil)
}

// Download starts a GET request for a release asset or source archive and
// returns the response with a 2xx status; the caller closes the body.
// Links outside the instance are allowed because Forgejo supports external
// assets; authTransport attaches the token only inside the instance root,
// also across redirects (§7.4).
func (c *Client) Download(ctx context.Context, link string) (*http.Response, error) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return nil, fjoerr.New(fjoerr.KindGeneral, "Forgejo returned an invalid download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fjoerr.Usage("invalid request: %v", err)
	}
	if c.ua != "" {
		req.Header.Set("User-Agent", c.ua)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if err := CheckResponse(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

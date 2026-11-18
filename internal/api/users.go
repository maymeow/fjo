package api

import (
	"context"
	"net/http"
	"time"
)

// User is the subset of Forgejo's User model fjo uses.
type User struct {
	ID        int64     `json:"id"`
	Login     string    `json:"login"`
	FullName  string    `json:"full_name"`
	Email     string    `json:"email"`
	HTMLURL   string    `json:"html_url"`
	AvatarURL string    `json:"avatar_url"`
	IsAdmin   bool      `json:"is_admin"`
	Created   time.Time `json:"created"`
}

// CurrentUser returns the user the token belongs to (GET /user). It is the
// minimal login verification request of §7.3.
func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	var u User
	if err := c.JSON(ctx, http.MethodGet, epCurrentUser.Expand(), nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ServerVersion returns the version string reported by GET /version
// (e.g. "17.0.1+gitea-1.22.0").
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.JSON(ctx, http.MethodGet, epServerVersion.Expand(), nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// Package api is fjo's own thin Forgejo REST client (IMPLEMENTATION §9,
// BACKLOG B-7). It handles base URLs, token authentication scoped to the
// instance, redirects, retries of safe requests, typed errors, pagination
// and sanitized logging. Models are hand-written for the subset fjo uses.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// Options configure a Client.
type Options struct {
	// BaseURL is the API root, e.g. https://git.example.com/api/v1.
	BaseURL string
	// Token is a personal access token; empty means anonymous.
	Token string
	// UserAgent is sent with every request.
	UserAgent string
	// CAFile is an optional PEM bundle trusted in addition to system roots.
	CAFile string
	// Logger receives sanitized request diagnostics (debug level).
	Logger *slog.Logger
	// Transport overrides the base HTTP transport (tests).
	Transport http.RoundTripper
	// MaxRetries for safe requests on 429/502/503/504 and network errors.
	// Zero means the default (2); negative disables retries.
	MaxRetries int
}

// Client talks to one Forgejo instance.
type Client struct {
	base    *url.URL // API root, path ends with "/"
	scope   *url.URL // instance root, path ends with "/"
	http    *http.Client
	ua      string
	log     *slog.Logger
	retries int
	backoff func(attempt int) time.Duration
}

// New creates a client. The token is attached only to requests inside the
// instance root derived from BaseURL.
func New(opts Options) (*Client, error) {
	base, err := url.Parse(opts.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, fmt.Errorf("invalid API URL %q", opts.BaseURL)
	}
	if base.User != nil {
		return nil, fmt.Errorf("API URL must not contain credentials")
	}
	base.RawQuery, base.Fragment = "", ""
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
		if base.RawPath != "" {
			base.RawPath += "/"
		}
	}
	scope := *base
	scope.Path = strings.TrimSuffix(base.Path, "api/v1/")
	scope.RawPath = ""
	if !strings.HasSuffix(scope.Path, "/") {
		scope.Path += "/"
	}

	transport := opts.Transport
	if transport == nil {
		transport, err = newBaseTransport(opts.CAFile)
		if err != nil {
			return nil, err
		}
	}

	retries := opts.MaxRetries
	switch {
	case retries == 0:
		retries = 2
	case retries < 0:
		retries = 0
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Client{
		base:  base,
		scope: &scope,
		http: &http.Client{
			Transport:     &authTransport{base: transport, token: opts.Token, scope: &scope},
			CheckRedirect: checkRedirect,
		},
		ua:      opts.UserAgent,
		log:     log,
		retries: retries,
		backoff: func(attempt int) time.Duration { return time.Duration(500<<attempt) * time.Millisecond },
	}, nil
}

// BaseURL returns the API root URL (with trailing slash).
func (c *Client) BaseURL() string { return c.base.String() }

// InstanceURL returns the instance root URL (with trailing slash).
func (c *Client) InstanceURL() string { return c.scope.String() }

// Host returns the host[:port] of the instance.
func (c *Client) Host() string { return c.base.Host }

// ResolveURL turns an endpoint path ("user", "/repos/a/b?x=1") or an
// absolute URL into a request URL. Absolute URLs must stay inside the
// instance root.
func (c *Client) ResolveURL(path string) (*url.URL, error) {
	if strings.Contains(path, "://") {
		u, err := url.Parse(path)
		if err != nil {
			return nil, fjoerr.Usage("invalid URL %q: %v", path, err)
		}
		if !inScope(u, c.scope) {
			return nil, fjoerr.Usage("URL %s is outside of %s", SanitizeURL(u), c.scope)
		}
		return u, nil
	}
	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fjoerr.Usage("invalid endpoint %q: %v", path, err)
	}
	return c.base.ResolveReference(rel), nil
}

// NewRequest builds a request for an endpoint path or in-scope URL.
func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u, err := c.ResolveURL(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fjoerr.Usage("invalid request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.ua != "" {
		req.Header.Set("User-Agent", c.ua)
	}
	return req, nil
}

// Do sends req and returns the response for any HTTP status. Only transport
// failures are returned as errors (classified); callers inspect the status
// or use CheckResponse. Safe requests are retried on transient failures.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		start := time.Now()
		resp, err := c.http.Do(req)
		if ctxErr := ctx.Err(); ctxErr != nil {
			if resp != nil {
				_ = resp.Body.Close()
			}
			return nil, ctxErr
		}
		retryable := attempt < c.retries && isReplayable(req)
		if err != nil {
			c.log.Debug("http error", "method", req.Method, "url", SanitizeURL(req.URL), "err", err.Error())
			if retryable && isTransientNetErr(err) {
				if werr := c.wait(ctx, attempt, 0); werr != nil {
					return nil, werr
				}
				if req, err = rewind(req); err != nil {
					return nil, err
				}
				continue
			}
			return nil, classifyTransport(err, c.base.Host)
		}
		c.log.Debug("http request",
			"method", req.Method, "url", SanitizeURL(req.URL), "status", resp.StatusCode,
			"duration", time.Since(start).Round(time.Millisecond).String(),
			"request_id", firstHeader(resp.Header, "X-Request-Id", "X-Gitea-Request-Id", "X-Forgejo-Request-Id"))
		if retryable && isRetryableStatus(resp.StatusCode) {
			after := retryAfter(resp.Header.Get("Retry-After"))
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
			_ = resp.Body.Close()
			if werr := c.wait(ctx, attempt, after); werr != nil {
				return nil, werr
			}
			if req, err = rewind(req); err != nil {
				return nil, err
			}
			continue
		}
		return resp, nil
	}
}

// CheckResponse returns nil for 2xx/3xx responses. Otherwise it reads (a
// bounded part of) the body, closes it and returns a classified *APIError.
func CheckResponse(resp *http.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}
	body := readLimited(resp.Body, maxErrorBody)
	_ = resp.Body.Close()
	return classifyStatus(newAPIError(resp, body))
}

// ErrorFromBody classifies an error response whose body the caller already
// read (used by `fjo api`, which also prints the body).
func ErrorFromBody(resp *http.Response, body []byte) error {
	return classifyStatus(newAPIError(resp, body))
}

// JSON performs a request with an optional JSON body and decodes a JSON
// response into out (if non-nil).
func (c *Client) JSON(ctx context.Context, method, path string, in, out any) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := c.NewRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if err := CheckResponse(resp); err != nil {
		return resp, err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp, fjoerr.Wrap(fjoerr.KindGeneral, err, "invalid response from %s %s", method, SanitizeURL(req.URL))
	}
	return resp, nil
}

func (c *Client) wait(ctx context.Context, attempt int, min time.Duration) error {
	d := c.backoff(attempt)
	if min > d {
		d = min
	}
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func isReplayable(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	}
	return false
}

func rewind(req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	r := req.Clone(req.Context())
	r.Body = body
	return r, nil
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func isTransientNetErr(err error) bool {
	// TLS and redirect-policy errors are permanent; plain network errors
	// (connection reset/refused, DNS hiccups) are worth one more try.
	c := classifyTransport(err, "")
	return fjoerr.KindOf(c) == fjoerr.KindUnavailable && !strings.Contains(c.Error(), "TLS")
}

// retryAfter parses a Retry-After header in seconds, capped at 10s.
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	if n > 10 {
		n = 10
	}
	return time.Duration(n) * time.Second
}

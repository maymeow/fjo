package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// Page describes pagination metadata of one response (§9.4).
type Page struct {
	Next  string // absolute URL of the next page, "" if none
	Last  string // absolute URL of the last page, "" if unknown
	Total int64  // X-Total-Count, -1 if not sent
}

// ParsePage reads the Link and X-Total-Count headers.
func ParsePage(h http.Header) Page {
	p := Page{Total: -1}
	if v := h.Get("X-Total-Count"); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			p.Total = n
		}
	}
	for _, link := range h.Values("Link") {
		for _, part := range strings.Split(link, ",") {
			target, rel, ok := parseLink(part)
			if !ok {
				continue
			}
			switch rel {
			case "next":
				p.Next = target
			case "last":
				p.Last = target
			}
		}
	}
	return p
}

func parseLink(s string) (target, rel string, ok bool) {
	s = strings.TrimSpace(s)
	end := strings.IndexByte(s, '>')
	if !strings.HasPrefix(s, "<") || end < 0 {
		return "", "", false
	}
	target = s[1:end]
	for _, param := range strings.Split(s[end+1:], ";") {
		k, v, found := strings.Cut(strings.TrimSpace(param), "=")
		if found && strings.EqualFold(k, "rel") {
			for _, r := range strings.Fields(strings.Trim(v, `"`)) {
				if r == "next" || r == "last" {
					return target, r, true
				}
			}
		}
	}
	return "", "", false
}

// DefaultMaxPages bounds page traversal when callers pass 0.
const DefaultMaxPages = 1000

// Pages issues a GET for path and follows rel="next" links, calling fn with
// every successful response (fn must not close the body; Pages does). It
// stops when there is no next page, after maxPages pages, when fn returns
// stop=true, or when ctx is cancelled. Next links must stay inside the
// instance root and are never requested twice.
func (c *Client) Pages(ctx context.Context, path string, header http.Header, maxPages int, fn func(*http.Response) (stop bool, err error)) error {
	if maxPages <= 0 {
		maxPages = DefaultMaxPages
	}
	u, err := c.ResolveURL(path)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	next := u.String()
	for page := 0; next != "" && page < maxPages; page++ {
		if seen[next] {
			return fjoerr.New(fjoerr.KindGeneral, "server returned a pagination loop at %s", sanitizeRaw(next))
		}
		seen[next] = true

		req, err := c.NewRequest(ctx, http.MethodGet, next, nil)
		if err != nil {
			return err
		}
		for k, v := range header {
			req.Header[k] = v
		}
		resp, err := c.Do(req)
		if err != nil {
			return err
		}
		if err := CheckResponse(resp); err != nil {
			return err
		}
		stop, ferr := fn(resp)
		_ = resp.Body.Close()
		if ferr != nil || stop {
			return ferr
		}

		next = ""
		if n := ParsePage(resp.Header).Next; n != "" {
			nu, err := resp.Request.URL.Parse(n)
			if err != nil || !inScope(nu, c.scope) {
				return fjoerr.New(fjoerr.KindGeneral, "refusing to follow pagination link outside of %s", c.scope)
			}
			next = nu.String()
		}
	}
	return nil
}

// List fetches up to limit items (all items when limit <= 0) from a paginated
// list endpoint. The page size is min(limit, 50); the server may cap it
// lower (MAX_RESPONSE_ITEMS), which pagination handles transparently.
func List[T any](ctx context.Context, c *Client, path string, query url.Values, limit int) ([]T, error) {
	return listMatching[T](ctx, c, path, query, limit, nil)
}

// listMatching applies the limit after client-side filtering.
func listMatching[T any](ctx context.Context, c *Client, path string, query url.Values, limit int, match func(T) bool) ([]T, error) {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	size := 50
	if match == nil && limit > 0 && limit < size {
		size = limit
	}
	q.Set("limit", strconv.Itoa(size))
	full := path
	if enc := q.Encode(); enc != "" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		full += sep + enc
	}

	var out []T
	err := c.Pages(ctx, full, nil, 0, func(resp *http.Response) (bool, error) {
		var items []T
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			return true, fjoerr.Wrap(fjoerr.KindGeneral, err, "invalid response from %s", SanitizeURL(resp.Request.URL))
		}
		for _, item := range items {
			if match == nil || match(item) {
				out = append(out, item)
				if limit > 0 && len(out) == limit {
					return true, nil
				}
			}
		}
		return len(items) == 0, nil
	})
	return out, err
}

func sanitizeRaw(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Sprintf("%q", "<invalid url>")
	}
	return SanitizeURL(u)
}

package api

import (
	"net/url"
	"strings"
)

// Endpoint is one Forgejo REST operation as named in the swagger spec
// ("GET", "/repos/{owner}/{repo}"). Every endpoint the client calls must be
// declared through endpoint() so a test can verify it against the pinned
// specs in test/fixtures/swagger (IMPLEMENTATION §9.1, BACKLOG B-8).
type Endpoint struct {
	Method string
	Path   string
}

var registry []Endpoint

func endpoint(method, path string) Endpoint {
	e := Endpoint{Method: method, Path: path}
	registry = append(registry, e)
	return e
}

// Endpoints returns all endpoints the client uses.
func Endpoints() []Endpoint { return append([]Endpoint(nil), registry...) }

// Expand fills the {placeholders} of the path in order with path-escaped
// args and returns a path relative to the API root (no leading slash).
func (e Endpoint) Expand(args ...string) string {
	var b strings.Builder
	rest := e.Path
	for _, a := range args {
		i := strings.IndexByte(rest, '{')
		j := strings.IndexByte(rest, '}')
		if i < 0 || j < i {
			panic("api: too many arguments for endpoint " + e.Path)
		}
		b.WriteString(rest[:i])
		b.WriteString(url.PathEscape(a))
		rest = rest[j+1:]
	}
	if strings.IndexByte(rest, '{') >= 0 {
		panic("api: missing arguments for endpoint " + e.Path)
	}
	b.WriteString(rest)
	return strings.TrimPrefix(b.String(), "/")
}

// Endpoints used by the client.
var (
	epCurrentUser   = endpoint("GET", "/user")
	epServerVersion = endpoint("GET", "/version")
)

var (
	epListIssues         = endpoint("GET", "/repos/{owner}/{repo}/issues")
	epCreateIssue        = endpoint("POST", "/repos/{owner}/{repo}/issues")
	epGetIssue           = endpoint("GET", "/repos/{owner}/{repo}/issues/{index}")
	epEditIssue          = endpoint("PATCH", "/repos/{owner}/{repo}/issues/{index}")
	epDeleteIssue        = endpoint("DELETE", "/repos/{owner}/{repo}/issues/{index}")
	epListIssueComments  = endpoint("GET", "/repos/{owner}/{repo}/issues/{index}/comments")
	epCreateIssueComment = endpoint("POST", "/repos/{owner}/{repo}/issues/{index}/comments")
	epListLabels         = endpoint("GET", "/repos/{owner}/{repo}/labels")
	epListMilestones     = endpoint("GET", "/repos/{owner}/{repo}/milestones")
	epAddIssueLabels     = endpoint("POST", "/repos/{owner}/{repo}/issues/{index}/labels")
	epRemoveIssueLabel   = endpoint("DELETE", "/repos/{owner}/{repo}/issues/{index}/labels/{identifier}")
	epPinIssue           = endpoint("POST", "/repos/{owner}/{repo}/issues/{index}/pin")
	epUnpinIssue         = endpoint("DELETE", "/repos/{owner}/{repo}/issues/{index}/pin")
)

var (
	epListPulls      = endpoint("GET", "/repos/{owner}/{repo}/pulls")
	epGetPull        = endpoint("GET", "/repos/{owner}/{repo}/pulls/{index}")
	epPullDiff       = endpoint("GET", "/repos/{owner}/{repo}/pulls/{index}.{diffType}")
	epPullFiles      = endpoint("GET", "/repos/{owner}/{repo}/pulls/{index}/files")
	epCommitStatuses = endpoint("GET", "/repos/{owner}/{repo}/statuses/{sha}")
)

var (
	epListReleases  = endpoint("GET", "/repos/{owner}/{repo}/releases")
	epLatestRelease = endpoint("GET", "/repos/{owner}/{repo}/releases/latest")
	epReleaseByTag  = endpoint("GET", "/repos/{owner}/{repo}/releases/tags/{tag}")
)

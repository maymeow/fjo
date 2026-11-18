package api

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	errRedirectDowngrade = errors.New("refusing redirect from https to http")
	errTooManyRedirects  = errors.New("stopped after 10 redirects")
)

// authTransport attaches the token only to requests inside the instance
// scope (same scheme, host and base path) and strips any Authorization
// header from requests leaving it, including redirect hops (§7.4, B-27).
type authTransport struct {
	base  http.RoundTripper
	token string
	scope *url.URL
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	in := inScope(req.URL, t.scope)
	if !in && req.Header.Get("Authorization") == "" {
		return t.base.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	switch {
	case !in:
		r.Header.Del("Authorization")
	case r.Header.Get("Authorization") == "" && t.token != "":
		r.Header.Set("Authorization", "token "+t.token)
	}
	return t.base.RoundTrip(r)
}

// inScope reports whether u is on the same origin as scope and below its
// path. scope.Path always ends with "/".
func inScope(u, scope *url.URL) bool {
	if u == nil || scope == nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, scope.Scheme) || !strings.EqualFold(u.Host, scope.Host) {
		return false
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	return strings.HasPrefix(p, scope.EscapedPath()) || p+"/" == scope.EscapedPath()
}

// checkRedirect limits redirect chains and refuses TLS downgrades. Header
// stripping for other origins is done by authTransport.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errTooManyRedirects
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errRedirectDowngrade
	}
	return nil
}

// newBaseTransport clones the default transport, which keeps proxy support
// (HTTP_PROXY, HTTPS_PROXY, NO_PROXY), and hardens TLS.
func newBaseTransport(caFile string) (http.RoundTripper, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("http.DefaultTransport is %T, not *http.Transport", http.DefaultTransport)
	}
	t := base.Clone()
	t.ResponseHeaderTimeout = 60 * time.Second
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read ca_file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_file %s contains no PEM certificates", caFile)
		}
		tlsCfg.RootCAs = pool
	}
	t.TLSClientConfig = tlsCfg
	return t, nil
}

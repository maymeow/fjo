package api

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// APIError is an HTTP error response from Forgejo (IMPLEMENTATION §9.3).
// URL never contains credentials or a query string.
type APIError struct {
	StatusCode int
	Method     string
	Message    string
	Errors     []string
	URL        string
	RequestID  string
}

func (e *APIError) Error() string {
	var b strings.Builder
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	fmt.Fprintf(&b, "%s (HTTP %d)", msg, e.StatusCode)
	if len(e.Errors) > 0 {
		b.WriteString(": " + strings.Join(e.Errors, "; "))
	}
	if e.URL != "" {
		fmt.Fprintf(&b, " [%s %s]", e.Method, e.URL)
	}
	return b.String()
}

// maxErrorBody limits how much of an error response is read.
const maxErrorBody = 64 << 10

// newAPIError builds an APIError from a response with status >= 400. body is
// the already-read response body (may be nil).
func newAPIError(resp *http.Response, body []byte) *APIError {
	e := &APIError{
		StatusCode: resp.StatusCode,
		RequestID:  firstHeader(resp.Header, "X-Request-Id", "X-Gitea-Request-Id", "X-Forgejo-Request-Id"),
	}
	if resp.Request != nil {
		e.Method = resp.Request.Method
		e.URL = SanitizeURL(resp.Request.URL)
	}
	var payload struct {
		Message string   `json:"message"`
		Errors  []string `json:"errors"`
	}
	if json.Unmarshal(body, &payload) == nil {
		e.Message = strings.TrimSpace(payload.Message)
		e.Errors = payload.Errors
	} else if s := strings.TrimSpace(string(body)); s != "" && len(s) < 200 && !strings.Contains(s, "<") {
		e.Message = s
	}
	return e
}

// classifyStatus wraps an APIError into the fjo error model.
func classifyStatus(e *APIError) error {
	kind := fjoerr.KindGeneral
	hint := ""
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		kind = fjoerr.KindAuth
		hint = "check your credentials with 'fjo auth status'"
	case e.StatusCode == http.StatusForbidden:
		kind = fjoerr.KindForbidden
		hint = "the token may lack the required scope or permission"
	case e.StatusCode == http.StatusNotFound:
		kind = fjoerr.KindNotFound
	case e.StatusCode == http.StatusConflict,
		e.StatusCode == http.StatusPreconditionFailed,
		e.StatusCode == http.StatusUnprocessableEntity:
		kind = fjoerr.KindConflict
	case e.StatusCode == http.StatusRequestEntityTooLarge:
		kind = fjoerr.KindConflict
		hint = "the server rejected the request size"
	case e.StatusCode == http.StatusTooManyRequests:
		kind = fjoerr.KindUnavailable
		hint = "rate limited by the server; try again later"
	case e.StatusCode >= 500:
		kind = fjoerr.KindUnavailable
	}
	err := fjoerr.Wrap(kind, e, "")
	if hint != "" {
		err = err.WithHint(hint)
	}
	return err
}

// classifyTransport converts errors from http.Client.Do into the fjo error
// model: TLS, timeout and network failures get their own kinds and hints.
// Callers handle context cancellation before calling it.
func classifyTransport(err error, host string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err // drop the URL; it is reported separately and sanitized
	}

	var (
		unknownCA *x509.UnknownAuthorityError
		hostErr   x509.HostnameError
		certErr   *tls.CertificateVerificationError
		invalid   x509.CertificateInvalidError
	)
	switch {
	case errors.As(err, &unknownCA), errors.As(err, &certErr), errors.As(err, &invalid):
		return fjoerr.Wrap(fjoerr.KindUnavailable, err, "TLS certificate of %s is not trusted", host).
			WithHint("for an internal CA set ca_file for this host in the fjo config")
	case errors.As(err, &hostErr):
		return fjoerr.Wrap(fjoerr.KindUnavailable, err, "TLS certificate does not match %s", host)
	}

	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fjoerr.Wrap(fjoerr.KindTimeout, err, "request to %s timed out", host)
	}
	if errors.Is(err, errRedirectDowngrade) || errors.Is(err, errTooManyRedirects) {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "request to %s failed", host)
	}
	return fjoerr.Wrap(fjoerr.KindUnavailable, err, "cannot reach %s", host)
}

// SanitizeURL renders u without user info, query or fragment.
func SanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.User = nil
	c.RawQuery = ""
	c.ForceQuery = false
	c.Fragment = ""
	c.RawFragment = ""
	return c.String()
}

func firstHeader(h http.Header, names ...string) string {
	for _, n := range names {
		if v := h.Get(n); v != "" {
			return v
		}
	}
	return ""
}

func readLimited(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, n))
}

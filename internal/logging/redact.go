// Package logging provides diagnostic logging that never emits secrets
// (IMPLEMENTATION §7.4, §17.3).
package logging

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Redacted replaces every secret in log output.
const Redacted = "[REDACTED]"

// Pattern-based redaction for values whose content is not known in advance.
var patterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Authorization / Proxy-Authorization headers: "Authorization: token abc".
	// Also matches JSON/quoted forms: "Authorization": "token abc".
	{regexp.MustCompile(`(?i)((?:proxy-)?authorization"?\s*[:=]\s*\[?"?)(?:(token|bearer|basic)\s+)?[^\s",;\]]+`), "${1}${2} " + Redacted},
	// Sensitive query parameters and key=value pairs.
	{regexp.MustCompile(`(?i)\b((?:access_)?token|password|passwd|secret|api_key|apikey|client_secret|sudo)=([^&\s"]+)`), "${1}=" + Redacted},
	// Credentials in URLs: scheme://user:pass@host.
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`), "${1}" + Redacted + "@"},
	// Common Forgejo/Gitea access token shape: 40 hex chars.
	{regexp.MustCompile(`\b[0-9a-f]{40}\b`), Redacted},
	// JWT shape: three base64url segments.
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`), Redacted},
}

// bareCredential matches "<token|bearer> <value>" outside an Authorization
// header or a key=value pair. Only credential-like values are redacted: the
// old rule treated any 8+ character word after "token" as a secret, so normal
// messages such as "no token provided" lost the word (BACKLOG B-36).
var bareCredential = regexp.MustCompile(`(?i)\b(token|bearer)(\s+)([A-Za-z0-9._~+/=-]{8,})`)

// looksLikeCredential requires a length and character mix that ordinary words
// after "token" (provided, required, rejected, settings) do not have. Known
// secrets are also registered exactly with Redactor.Add.
func looksLikeCredential(v string) bool {
	if len(v) < 20 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' {
			return true
		}
	}
	return false
}

func redactBareCredentials(s string) string {
	return bareCredential.ReplaceAllStringFunc(s, func(m string) string {
		sub := bareCredential.FindStringSubmatch(m)
		if !looksLikeCredential(sub[3]) {
			return m
		}
		return sub[1] + sub[2] + Redacted
	})
}

// Redactor removes registered secret values and known secret patterns.
// It is safe for concurrent use.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

// NewRedactor returns a redactor with no registered secrets.
func NewRedactor() *Redactor { return &Redactor{} }

// minSecretLen avoids redacting short, common substrings.
const minSecretLen = 4

// Add registers exact secret values (tokens read from env or keyring) that must
// never appear in output, even if they do not match a pattern.
func (r *Redactor) Add(secrets ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range secrets {
		if len(s) >= minSecretLen {
			r.secrets = append(r.secrets, s)
		}
	}
	// Longest first so overlapping secrets are fully replaced.
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
}

// String returns s with all secrets replaced.
func (r *Redactor) String(s string) string {
	if r != nil {
		r.mu.RLock()
		for _, secret := range r.secrets {
			s = strings.ReplaceAll(s, secret, Redacted)
		}
		r.mu.RUnlock()
	}
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return redactBareCredentials(s)
}

package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// NormalizeHost converts a host ID or URL into the canonical host ID defined
// in IMPLEMENTATION §6.2: lowercase ASCII hostname, non-default port kept,
// scheme dropped, base path kept without trailing slash.
//
// Accepted inputs include "git.example.com", "Git.Example.com:443",
// "https://example.com/forgejo/", "http://git.internal:3000" and
// "git.example.com:3000".
//
// An "/api/v1" suffix is NOT stripped: it would be ambiguous with a real
// subpath, so callers pass instance URLs, not API URLs.
func NormalizeHost(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("empty host")
	}

	scheme := "https"
	if i := strings.Index(s, "://"); i >= 0 {
		scheme = strings.ToLower(s[:i])
		if scheme != "https" && scheme != "http" {
			return "", fmt.Errorf("unsupported scheme %q in host %q", scheme, input)
		}
	} else {
		s = scheme + "://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid host %q: %w", input, err)
	}
	if u.User != nil {
		return "", fmt.Errorf("host %q must not contain credentials", input)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("host %q must not contain a query or fragment", input)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("invalid host %q: missing hostname", input)
	}
	if ip := net.ParseIP(hostname); ip == nil {
		ascii, err := idna.Lookup.ToASCII(hostname)
		if err != nil {
			return "", fmt.Errorf("invalid hostname %q: %w", hostname, err)
		}
		hostname = strings.ToLower(ascii)
	} else {
		hostname = ip.String()
	}

	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}

	id := hostname
	if strings.Contains(hostname, ":") { // IPv6 literal
		id = "[" + hostname + "]"
	}
	if port != "" {
		id += ":" + port
	}

	path := strings.TrimRight(u.EscapedPath(), "/")
	if strings.Contains(path, "//") {
		return "", fmt.Errorf("invalid path in host %q", input)
	}
	return id + path, nil
}

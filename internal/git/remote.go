package git

import (
	"fmt"
	"net/url"
	"strings"
)

// URL is a parsed git remote URL (IMPLEMENTATION §8.1).
type URL struct {
	// Scheme is "https", "http", "ssh" or "git". scp-like remotes
	// (git@host:owner/repo.git) are reported as "ssh".
	Scheme string
	// Host is the lowercase hostname without port or brackets.
	Host string
	// Port is the explicit port, or "".
	Port string
	// Path is the repository path without leading slash, e.g.
	// "owner/repo.git" or "forgejo/owner/repo.git".
	Path string
}

// IsSSH reports whether the remote is reached over SSH.
func (u URL) IsSSH() bool { return u.Scheme == "ssh" }

// ParseURL parses the remote URL formats git supports for network remotes:
//
//	https://git.example.com/owner/repo.git
//	ssh://git@git.example.com:2222/owner/repo.git
//	git@git.example.com:owner/repo.git   (scp-like)
//	git://git.example.com/owner/repo.git
//
// Local paths and file:// URLs are rejected. Credentials embedded in
// https URLs are dropped and never returned.
func ParseURL(raw string) (URL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return URL{}, fmt.Errorf("empty remote URL")
	}
	if i := strings.Index(s, "://"); i > 0 {
		return parseSchemeURL(s, strings.ToLower(s[:i]))
	}
	return parseSCP(s)
}

func parseSchemeURL(s, scheme string) (URL, error) {
	switch scheme {
	case "git+ssh", "ssh+git":
		scheme = "ssh"
	case "https", "http", "ssh", "git":
	default:
		return URL{}, fmt.Errorf("unsupported remote URL scheme %q", scheme)
	}
	u, err := url.Parse(s)
	if err != nil {
		return URL{}, fmt.Errorf("invalid remote URL: %w", err)
	}
	if u.Hostname() == "" {
		return URL{}, fmt.Errorf("remote URL has no host")
	}
	return URL{
		Scheme: scheme,
		Host:   strings.ToLower(u.Hostname()),
		Port:   u.Port(),
		Path:   cleanPath(u.Path),
	}, nil
}

// parseSCP handles "[user@]host:path". A colon must come before any slash,
// otherwise the string is a local path.
func parseSCP(s string) (URL, error) {
	colon := strings.IndexByte(s, ':')
	// IPv6 literal: git@[::1]:owner/repo — the separator follows the ']'.
	if open := strings.IndexByte(s, '['); open >= 0 && open < colon {
		if end := strings.IndexByte(s, ']'); end > open && end+1 < len(s) && s[end+1] == ':' {
			colon = end + 1
		}
	}
	slash := strings.IndexByte(s, '/')
	if colon <= 0 || (slash >= 0 && slash < colon) {
		return URL{}, fmt.Errorf("unsupported remote %q: local paths are not Forgejo remotes", s)
	}
	hostPart, path := s[:colon], s[colon+1:]
	if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
		hostPart = hostPart[at+1:]
	}
	hostPart = strings.Trim(hostPart, "[]")
	// A single letter before the colon is a Windows drive (C:\repo).
	if hostPart == "" || (len(hostPart) == 1 && len(path) > 0 && (path[0] == '\\' || path[0] == '/')) {
		return URL{}, fmt.Errorf("unsupported remote %q: local paths are not Forgejo remotes", s)
	}
	return URL{Scheme: "ssh", Host: strings.ToLower(hostPart), Path: cleanPath(path)}, nil
}

func cleanPath(p string) string {
	return strings.Trim(p, "/")
}

// TrimGitSuffix removes a trailing ".git" from a repository name.
func TrimGitSuffix(name string) string { return strings.TrimSuffix(name, ".git") }

// Package reporef identifies Forgejo repositories: parsing "owner/repo"
// arguments and mapping git remote URLs to a host ID (IMPLEMENTATION §6.2,
// §8.1, §8.2).
package reporef

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/git"
)

// Ref is a repository on a Forgejo instance.
type Ref struct {
	Host  string // canonical host ID
	Owner string
	Name  string
}

// FullName returns "owner/name".
func (r Ref) FullName() string { return r.Owner + "/" + r.Name }

// String returns "host/owner/name".
func (r Ref) String() string { return r.Host + "/" + r.FullName() }

// Equal compares refs; owner and name are case-insensitive like Forgejo.
func (r Ref) Equal(o Ref) bool {
	return r.Host == o.Host && strings.EqualFold(r.Owner, o.Owner) && strings.EqualFold(r.Name, o.Name)
}

// Parse reads a repository argument:
//
//	owner/repo                      -> defaultHost (resolved lazily)
//	host/owner/repo                 -> host may contain a port or base path
//	https://host[/base]/owner/repo  -> URL, matched against configured hosts
//
// defaultHost is called only for the two-segment form.
func Parse(s string, cfg *config.Config, defaultHost func() (string, error)) (Ref, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") || isSCP(s) {
		u, err := git.ParseURL(s)
		if err != nil {
			return Ref{}, fmt.Errorf("invalid repository %q: %w", s, err)
		}
		return FromURL(u, cfg)
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	for _, p := range parts {
		if p == "" {
			return Ref{}, invalid(s)
		}
	}
	switch {
	case len(parts) == 2:
		host, err := defaultHost()
		if err != nil {
			return Ref{}, err
		}
		return newRef(host, parts[0], parts[1], s)
	case len(parts) > 2:
		host, err := config.NormalizeHost(strings.Join(parts[:len(parts)-2], "/"))
		if err != nil {
			return Ref{}, fmt.Errorf("invalid repository %q: %w", s, err)
		}
		return newRef(host, parts[len(parts)-2], parts[len(parts)-1], s)
	}
	return Ref{}, invalid(s)
}

func isSCP(s string) bool {
	colon := strings.IndexByte(s, ':')
	slash := strings.IndexByte(s, '/')
	return strings.Contains(s, "@") && colon > 0 && (slash < 0 || colon < slash)
}

func invalid(s string) error {
	return fmt.Errorf("invalid repository %q: expected OWNER/REPO or HOST/OWNER/REPO", s)
}

func newRef(host, owner, name, input string) (Ref, error) {
	name = git.TrimGitSuffix(name)
	if !validSegment(owner) || !validSegment(name) {
		return Ref{}, invalid(input)
	}
	return Ref{Host: host, Owner: owner, Name: name}, nil
}

// validSegment accepts Forgejo user/repo names: letters, digits, '-', '_',
// '.'; no "." or ".." and no path separators.
func validSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// FromURL maps a remote URL to a repository.
//
// HTTP(S): the configured host whose origin matches and whose base path is
// the longest prefix of the URL path wins; the rest must be owner/repo.
// Without a configured match the origin is the host ID.
//
// SSH: a host whose ssh_hosts lists "host:port" or "host" wins; otherwise
// the only configured host with the same hostname; otherwise the SSH
// hostname itself. Several configured hosts with that hostname are
// ambiguous and reported, not guessed (§6.2).
func FromURL(u git.URL, cfg *config.Config) (Ref, error) {
	var (
		host string
		path = u.Path
		err  error
	)
	switch u.Scheme {
	case "https", "http":
		host, path, err = matchHTTP(u, cfg)
	case "ssh", "git":
		host, path, err = matchSSH(u, cfg)
	default:
		err = fmt.Errorf("unsupported remote scheme %q", u.Scheme)
	}
	if err != nil {
		return Ref{}, err
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return Ref{}, fmt.Errorf("remote path %q is not OWNER/REPO on %s", u.Path, host)
	}
	return newRef(host, parts[0], parts[1], u.Path)
}

func origin(scheme, host, port string) string {
	h := host
	if strings.Contains(h, ":") {
		h = "[" + h + "]"
	}
	if port != "" {
		h = net.JoinHostPort(host, port)
	}
	return scheme + "://" + h
}

func matchHTTP(u git.URL, cfg *config.Config) (string, string, error) {
	base, err := config.NormalizeHost(origin(u.Scheme, u.Host, u.Port))
	if err != nil {
		return "", "", err
	}
	best, rest := "", u.Path
	for _, id := range cfg.HostIDs() {
		if id != base && !strings.HasPrefix(id, base+"/") {
			continue
		}
		sub := strings.TrimPrefix(id, base) // "" or "/forgejo"
		sub = strings.TrimPrefix(sub, "/")
		if sub != "" && !strings.HasPrefix(u.Path, sub+"/") {
			continue
		}
		if len(id) > len(best) {
			best = id
			rest = strings.TrimPrefix(strings.TrimPrefix(u.Path, sub), "/")
		}
	}
	if best == "" {
		return base, u.Path, nil
	}
	return best, rest, nil
}

func matchSSH(u git.URL, cfg *config.Config) (string, string, error) {
	keys := []string{strings.ToLower(u.Host)}
	if u.Port != "" {
		keys = append([]string{strings.ToLower(net.JoinHostPort(u.Host, u.Port))}, keys...)
	}
	for _, key := range keys {
		for _, id := range cfg.HostIDs() {
			for _, s := range cfg.Hosts[id].SSHHosts {
				if strings.EqualFold(s, key) {
					return id, stripBase(id, u.Path), nil
				}
			}
		}
	}

	var same []string
	for _, id := range cfg.HostIDs() {
		if hostnameOf(id) == strings.ToLower(u.Host) {
			same = append(same, id)
		}
	}
	switch len(same) {
	case 0:
		id, err := config.NormalizeHost(u.Host)
		return id, u.Path, err
	case 1:
		return same[0], stripBase(same[0], u.Path), nil
	}
	sort.Strings(same)
	return "", "", fmt.Errorf("SSH host %s matches several configured hosts (%s); add it to ssh_hosts of one of them",
		u.Host, strings.Join(same, ", "))
}

// hostnameOf returns the hostname part of a host ID (no port, no path).
func hostnameOf(id string) string {
	h := id
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	if hh, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(hh, "[]")
	}
	return strings.Trim(h, "[]")
}

// stripBase removes a host's base path from an SSH path, which Forgejo
// normally omits but some setups include.
func stripBase(id, path string) string {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		if b := id[i+1:]; strings.HasPrefix(path, b+"/") {
			return strings.TrimPrefix(path, b+"/")
		}
	}
	return path
}

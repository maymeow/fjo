package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maymeow/fjo/internal/iostreams"
)

// Validate checks the configuration for structural and semantic errors and
// returns all problems found, joined.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Version != CurrentVersion {
		add("unsupported config version %d (this fjo supports version %d)", c.Version, CurrentVersion)
	}

	if c.Defaults.Host != "" {
		if id, err := NormalizeHost(c.Defaults.Host); err != nil {
			add("defaults.host: %v", err)
		} else if id != c.Defaults.Host {
			add("defaults.host: %q is not a canonical host ID, use %q", c.Defaults.Host, id)
		}
	}
	if err := validateGitProtocol(c.Defaults.GitProtocol); err != nil {
		add("defaults.git_protocol: %v", err)
	}
	if c.Output.Color != "" {
		if _, ok := iostreams.ParseColorMode(c.Output.Color); !ok {
			add("output.color: %q is not one of auto, always, never", c.Output.Color)
		}
	}

	ids := make([]string, 0, len(c.Hosts))
	for id := range c.Hosts {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	sshOwner := map[string]string{}
	for _, id := range ids {
		h := c.Hosts[id]
		prefix := "hosts." + id
		norm, err := NormalizeHost(id)
		if err != nil {
			add("%s: %v", prefix, err)
			continue
		}
		if norm != id {
			add("%s: not a canonical host ID, use %q", prefix, norm)
		}
		if h == nil {
			continue
		}
		if err := validateGitProtocol(h.GitProtocol); err != nil {
			add("%s.git_protocol: %v", prefix, err)
		}
		if h.APIURL != "" {
			if err := validateAPIURL(h.APIURL, h.InsecureHTTP); err != nil {
				add("%s.api_url: %v", prefix, err)
			}
		}
		if h.CAFile != "" && !filepath.IsAbs(h.CAFile) {
			add("%s.ca_file: %q must be an absolute path", prefix, h.CAFile)
		}
		if h.ActiveUser != "" && !contains(h.Users, h.ActiveUser) {
			add("%s.active_user: %q is not listed in users", prefix, h.ActiveUser)
		}
		seen := map[string]bool{}
		for _, u := range h.Users {
			if strings.TrimSpace(u) == "" {
				add("%s.users: empty username", prefix)
			} else if seen[u] {
				add("%s.users: duplicate user %q", prefix, u)
			}
			seen[u] = true
		}
		for _, s := range h.SSHHosts {
			key := strings.ToLower(s)
			if owner, dup := sshOwner[key]; dup && owner != id {
				add("%s.ssh_hosts: %q is already mapped to host %q", prefix, s, owner)
			}
			sshOwner[key] = id
		}
	}

	return errors.Join(errs...)
}

func validateGitProtocol(p string) error {
	switch p {
	case "", "ssh", "https":
		return nil
	}
	return fmt.Errorf("%q is not one of ssh, https", p)
}

func validateAPIURL(raw string, insecureHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Host == "" {
		return fmt.Errorf("%q is not an absolute URL", raw)
	}
	if u.User != nil {
		return fmt.Errorf("must not contain credentials")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !insecureHTTP {
			return fmt.Errorf("plain http requires insecure_http: true for this host")
		}
	default:
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

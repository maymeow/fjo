package config

import (
	"fmt"
	"slices"
	"sort"
)

// APIURL returns the API root URL for a host: the configured api_url, or
// https://<host ID>/api/v1 (http:// when insecure_http is set). h may be nil
// for a host that is not configured yet.
func APIURL(id string, h *Host) string {
	if h != nil && h.APIURL != "" {
		return h.APIURL
	}
	scheme := "https"
	if h != nil && h.InsecureHTTP {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s/api/v1", scheme, id)
}

// HostIDs returns the configured host IDs in sorted order.
func (c *Config) HostIDs() []string {
	if c == nil {
		return nil
	}
	ids := make([]string, 0, len(c.Hosts))
	for id := range c.Hosts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// AddUser records user as logged in to host id and makes it the active user.
// The host entry is created when missing. The first logged-in host becomes
// the default host.
func (c *Config) AddUser(id, user string) *Host {
	if c.Hosts == nil {
		c.Hosts = map[string]*Host{}
	}
	h := c.Hosts[id]
	if h == nil {
		h = &Host{}
		c.Hosts[id] = h
	}
	if !slices.Contains(h.Users, user) {
		h.Users = append(h.Users, user)
	}
	h.ActiveUser = user
	if c.Defaults.Host == "" {
		c.Defaults.Host = id
	}
	return h
}

// RemoveUser forgets user on host id. When the active user is removed the
// next remaining user becomes active. When no users remain the host entry is
// deleted (and stops being the default host). It reports the new active user
// ("" if none) and whether user was present.
func (c *Config) RemoveUser(id, user string) (newActive string, removed bool) {
	h := c.Host(id)
	if h == nil {
		return "", false
	}
	i := slices.Index(h.Users, user)
	if i < 0 {
		return h.ActiveUser, false
	}
	h.Users = slices.Delete(h.Users, i, i+1)
	if h.ActiveUser == user {
		h.ActiveUser = ""
		if len(h.Users) > 0 {
			h.ActiveUser = h.Users[0]
		}
	}
	if len(h.Users) == 0 {
		delete(c.Hosts, id)
		if c.Defaults.Host == id {
			c.Defaults.Host = ""
		}
	}
	return h.ActiveUser, true
}

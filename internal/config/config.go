// Package config loads, validates, resolves and persists fjo's nonsecret
// user configuration (IMPLEMENTATION §6).
//
// Tokens are never stored here; see the credentials package.
package config

// CurrentVersion is the configuration schema version this build writes.
const CurrentVersion = 1

// Config is the on-disk user configuration (config.yaml).
type Config struct {
	Version  int              `yaml:"version"`
	Defaults Defaults         `yaml:"defaults,omitempty"`
	Hosts    map[string]*Host `yaml:"hosts,omitempty"`
	Output   Output           `yaml:"output,omitempty"`
}

// Defaults are user-level defaults applied when no flag or env var is set.
type Defaults struct {
	Host        string `yaml:"host,omitempty"`
	GitProtocol string `yaml:"git_protocol,omitempty"`
	Editor      string `yaml:"editor,omitempty"`
	Pager       string `yaml:"pager,omitempty"`
}

// Host is the configuration of one Forgejo instance, keyed by host ID (§6.2).
type Host struct {
	APIURL       string   `yaml:"api_url,omitempty"`
	GitProtocol  string   `yaml:"git_protocol,omitempty"`
	ActiveUser   string   `yaml:"active_user,omitempty"`
	Users        []string `yaml:"users,omitempty"`
	SSHHosts     []string `yaml:"ssh_hosts,omitempty"`
	InsecureHTTP bool     `yaml:"insecure_http,omitempty"`
}

// Output holds presentation preferences.
type Output struct {
	Color string `yaml:"color,omitempty"`
}

// New returns an empty configuration at the current schema version.
func New() *Config {
	return &Config{Version: CurrentVersion, Hosts: map[string]*Host{}}
}

// Host returns the entry for a host ID, or nil.
func (c *Config) Host(id string) *Host {
	if c == nil || c.Hosts == nil {
		return nil
	}
	return c.Hosts[id]
}

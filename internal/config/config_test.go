package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const sampleConfig = `version: 1

defaults:
  host: git.example.com
  git_protocol: ssh
  editor: code --wait
  pager: auto

hosts:
  git.example.com:
    api_url: https://git.example.com/api/v1
    git_protocol: ssh
    active_user: developer
    users:
      - developer
      - deploy-bot
    ssh_hosts:
      - git.example.com
      - forgejo-work

  example.com/forgejo:
    api_url: https://example.com/forgejo/api/v1
    git_protocol: https
    active_user: developer
    users: [developer]

  git.internal:3000:
    api_url: http://git.internal:3000/api/v1
    insecure_http: true
    active_user: admin
    users: [admin]

output:
  color: auto
`

func TestParseSample(t *testing.T) {
	cfg, err := Parse(strings.NewReader(sampleConfig))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Hostname keys with dots and colons must stay intact (the reason Viper was dropped).
	for _, id := range []string{"git.example.com", "example.com/forgejo", "git.internal:3000"} {
		if cfg.Host(id) == nil {
			t.Errorf("host %q missing; hosts = %v", id, keys(cfg.Hosts))
		}
	}
	if got := cfg.Host("git.example.com").ActiveUser; got != "developer" {
		t.Errorf("active_user = %q", got)
	}
	if !cfg.Host("git.internal:3000").InsecureHTTP {
		t.Error("insecure_http not parsed")
	}
}

func TestParseEmpty(t *testing.T) {
	cfg, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if cfg.Version != CurrentVersion || cfg.Hosts == nil {
		t.Errorf("empty config not defaulted: %+v", cfg)
	}
}

func TestParseRejects(t *testing.T) {
	tests := map[string]string{
		"unknown key":               "version: 1\nfoo: bar\n",
		"wrong version":             "version: 2\n",
		"missing version":           "hosts: {}\n",
		"non-canonical host":        "version: 1\nhosts:\n  Git.Example.com: {}\n",
		"http without opt-in":       "version: 1\nhosts:\n  git.internal:3000:\n    api_url: http://git.internal:3000/api/v1\n",
		"active user unknown":       "version: 1\nhosts:\n  a.example:\n    active_user: x\n    users: [y]\n",
		"duplicate user":            "version: 1\nhosts:\n  a.example:\n    users: [x, x]\n",
		"bad protocol":              "version: 1\ndefaults:\n  git_protocol: ftp\n",
		"bad color":                 "version: 1\noutput:\n  color: rainbow\n",
		"credentials in url":        "version: 1\nhosts:\n  a.example:\n    api_url: https://u:p@a.example/api/v1\n",
		"ssh alias twice":           "version: 1\nhosts:\n  a.example:\n    ssh_hosts: [s]\n  b.example:\n    ssh_hosts: [S]\n",
		"default host noncanonical": "version: 1\ndefaults:\n  host: https://a.example/\n",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(in)); err == nil {
				t.Errorf("expected error for:\n%s", in)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("version = %d", cfg.Version)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "fjo")
	path := filepath.Join(dir, FileName)

	cfg, err := Parse(strings.NewReader(sampleConfig))
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Hosts) != 3 || got.Host("git.example.com").Users[1] != "deploy-bot" {
		t.Errorf("round trip mismatch: %+v", got.Hosts)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("config mode = %v, want 0600", fi.Mode().Perm())
		}
	}
}

func TestSaveRefusesInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	cfg := New()
	cfg.Output.Color = "rainbow"
	if err := Save(path, cfg); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("invalid config must not be written")
	}
}

func TestDir(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	got, _ := Dir(env(map[string]string{"FJO_CONFIG_DIR": "/a", "XDG_CONFIG_HOME": "/b"}))
	if got != "/a" {
		t.Errorf("FJO_CONFIG_DIR not preferred: %q", got)
	}
	got, _ = Dir(env(map[string]string{"XDG_CONFIG_HOME": "/b"}))
	if got != filepath.Join("/b", "fjo") {
		t.Errorf("XDG_CONFIG_HOME not used: %q", got)
	}
	got, err := Dir(env(nil))
	if err != nil || filepath.Base(got) != "fjo" {
		t.Errorf("default dir = %q, %v", got, err)
	}
}

func TestResolveSettingsPrecedence(t *testing.T) {
	cfg := New()
	cfg.Defaults.Host = "config.example"
	cfg.Defaults.Editor = "vim"
	cfg.Output.Color = "never"

	env := map[string]string{"FJO_HOST": "env.example", "EDITOR": "nano"}
	getenv := func(k string) string { return env[k] }

	s := ResolveSettings(cfg, Overrides{Host: "flag.example"}, getenv)
	if s.Host != (Value{"flag.example", SourceFlag}) {
		t.Errorf("flag should win: %+v", s.Host)
	}

	s = ResolveSettings(cfg, Overrides{}, getenv)
	if s.Host != (Value{"env.example", SourceEnv}) {
		t.Errorf("env should beat config: %+v", s.Host)
	}
	if s.Editor != (Value{"vim", SourceUser}) {
		t.Errorf("config editor should beat EDITOR: %+v", s.Editor)
	}
	if s.Color != (Value{"never", SourceUser}) {
		t.Errorf("color from config: %+v", s.Color)
	}

	delete(env, "FJO_HOST")
	s = ResolveSettings(New(), Overrides{}, getenv)
	if s.Host.Source != SourceNotFound {
		t.Errorf("host should be unset: %+v", s.Host)
	}
	if s.Color != (Value{"auto", SourceDefault}) {
		t.Errorf("default color: %+v", s.Color)
	}
	if s.Editor != (Value{"nano", SourceEnv}) {
		t.Errorf("EDITOR fallback: %+v", s.Editor)
	}
}

func keys(m map[string]*Host) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

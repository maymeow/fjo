package reporef

import (
	"errors"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/git"
)

func testConfig() *config.Config {
	cfg := config.New()
	cfg.Hosts["git.example.com"] = &config.Host{SSHHosts: []string{"forgejo-ssh", "ssh.example.com:2222"}}
	cfg.Hosts["example.com/forgejo"] = &config.Host{}
	cfg.Hosts["example.com:3000"] = &config.Host{}
	return cfg
}

func defaultHost(h string) func() (string, error) {
	return func() (string, error) { return h, nil }
}

func TestParse(t *testing.T) {
	cfg := testConfig()
	tests := []struct {
		in   string
		want Ref
	}{
		{"owner/repo", Ref{"default.example.com", "owner", "repo"}},
		{"owner/repo.git", Ref{"default.example.com", "owner", "repo"}},
		{"git.example.com/owner/repo", Ref{"git.example.com", "owner", "repo"}},
		{"Git.Example.com:443/owner/repo", Ref{"git.example.com", "owner", "repo"}},
		{"example.com/forgejo/owner/repo", Ref{"example.com/forgejo", "owner", "repo"}},
		{"https://example.com/forgejo/owner/repo", Ref{"example.com/forgejo", "owner", "repo"}},
		{"https://git.example.com/owner/my.repo.git", Ref{"git.example.com", "owner", "my.repo"}},
		{"git@forgejo-ssh:owner/repo.git", Ref{"git.example.com", "owner", "repo"}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in, cfg, defaultHost("default.example.com"))
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{"", "repo", "owner/", "/repo", "owner//repo", "owner/re po", "owner/..", "https://git.example.com/only-owner"} {
		if got, err := Parse(in, testConfig(), defaultHost("h")); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", in, got)
		}
	}
	// The default host is only needed for OWNER/REPO.
	wantErr := errors.New("no default host")
	if _, err := Parse("owner/repo", nil, func() (string, error) { return "", wantErr }); !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if _, err := Parse("h.example.com/owner/repo", nil, func() (string, error) { return "", wantErr }); err != nil {
		t.Errorf("HOST/OWNER/REPO should not need a default host: %v", err)
	}
}

func TestFromURL(t *testing.T) {
	cfg := testConfig()
	tests := []struct {
		in   string
		want Ref
	}{
		{"https://git.example.com/owner/repo.git", Ref{"git.example.com", "owner", "repo"}},
		{"https://GIT.example.com:443/owner/repo", Ref{"git.example.com", "owner", "repo"}},
		{"https://example.com/forgejo/owner/repo.git", Ref{"example.com/forgejo", "owner", "repo"}},
		{"https://example.com:3000/owner/repo.git", Ref{"example.com:3000", "owner", "repo"}},
		{"https://unknown.example.org/owner/repo", Ref{"unknown.example.org", "owner", "repo"}},
		{"git@git.example.com:owner/repo.git", Ref{"git.example.com", "owner", "repo"}},
		{"ssh://git@ssh.example.com:2222/owner/repo.git", Ref{"git.example.com", "owner", "repo"}},
		{"forgejo-ssh:owner/repo", Ref{"git.example.com", "owner", "repo"}},
		{"git@codeberg.org:owner/repo.git", Ref{"codeberg.org", "owner", "repo"}},
	}
	for _, tt := range tests {
		u, err := git.ParseURL(tt.in)
		if err != nil {
			t.Fatalf("ParseURL(%q): %v", tt.in, err)
		}
		got, err := FromURL(u, cfg)
		if err != nil {
			t.Errorf("FromURL(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("FromURL(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestFromURLSSHAmbiguous(t *testing.T) {
	cfg := config.New()
	cfg.Hosts["example.com/a"] = &config.Host{}
	cfg.Hosts["example.com/b"] = &config.Host{}
	u, _ := git.ParseURL("git@example.com:owner/repo.git")
	_, err := FromURL(u, cfg)
	if err == nil || !strings.Contains(err.Error(), "ssh_hosts") {
		t.Errorf("err = %v, want an ambiguity error mentioning ssh_hosts", err)
	}

	// An ssh_hosts entry resolves the ambiguity.
	cfg.Hosts["example.com/b"].SSHHosts = []string{"example.com"}
	got, err := FromURL(u, cfg)
	if err != nil || got.Host != "example.com/b" {
		t.Errorf("FromURL = %+v, %v", got, err)
	}
}

func TestFromURLSSHStripsBasePath(t *testing.T) {
	cfg := config.New()
	cfg.Hosts["example.com/forgejo"] = &config.Host{}
	u, _ := git.ParseURL("git@example.com:forgejo/owner/repo.git")
	got, err := FromURL(u, cfg)
	if err != nil || got != (Ref{"example.com/forgejo", "owner", "repo"}) {
		t.Errorf("FromURL = %+v, %v", got, err)
	}
}

func TestFromURLWrongPath(t *testing.T) {
	for _, in := range []string{
		"https://git.example.com/owner",
		"https://git.example.com/a/b/c",
		"git@git.example.com:owner",
	} {
		u, _ := git.ParseURL(in)
		if got, err := FromURL(u, testConfig()); err == nil {
			t.Errorf("FromURL(%q) = %+v, want an error", in, got)
		}
	}
}

func TestRefHelpers(t *testing.T) {
	r := Ref{"git.example.com", "Owner", "Repo"}
	if r.FullName() != "Owner/Repo" || r.String() != "git.example.com/Owner/Repo" {
		t.Errorf("FullName/String = %q / %q", r.FullName(), r.String())
	}
	if !r.Equal(Ref{"git.example.com", "owner", "repo"}) {
		t.Error("Equal should ignore case of owner and name")
	}
	if r.Equal(Ref{"other.example.com", "Owner", "Repo"}) {
		t.Error("Equal must compare hosts")
	}
}

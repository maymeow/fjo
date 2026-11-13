package config

import "testing"

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"git.example.com", "git.example.com"},
		{"Git.Example.COM", "git.example.com"},
		{"  git.example.com  ", "git.example.com"},
		{"https://git.example.com", "git.example.com"},
		{"https://git.example.com/", "git.example.com"},
		{"https://git.example.com:443", "git.example.com"},
		{"git.example.com:443", "git.example.com"},
		{"git.example.com:3000", "git.example.com:3000"},
		{"http://git.internal:80", "git.internal"},
		{"http://git.internal:3000/", "git.internal:3000"},
		{"https://example.com/forgejo/", "example.com/forgejo"},
		{"example.com/forgejo", "example.com/forgejo"},
		{"https://Example.com/Forgejo", "example.com/Forgejo"}, // path is case-sensitive
		{"https://bücher.example", "xn--bcher-kva.example"},
		{"https://[::1]:3000", "[::1]:3000"},
		{"127.0.0.1:3000", "127.0.0.1:3000"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeHost(tt.in)
			if err != nil {
				t.Fatalf("NormalizeHost(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeHostErrors(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"ftp://git.example.com",
		"https://user:pass@git.example.com",
		"https://git.example.com/?x=1",
		"https://git.example.com/#frag",
		"https://",
		"https://git.example.com//double",
	} {
		if got, err := NormalizeHost(in); err == nil {
			t.Errorf("NormalizeHost(%q) = %q, expected error", in, got)
		}
	}
}

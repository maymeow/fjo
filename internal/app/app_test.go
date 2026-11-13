package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

func newFactory(env map[string]string) *Factory {
	io, _, _, _ := iostreams.Test()
	return New(io, func(k string) string { return env[k] }, build.Info{})
}

func TestConfigLoadsAndCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\ndefaults:\n  host: git.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFactory(map[string]string{"FJO_CONFIG_DIR": dir})

	got, err := f.ConfigPath()
	if err != nil || got != path {
		t.Fatalf("ConfigPath = %q, %v", got, err)
	}
	cfg, err := f.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Defaults.Host != "git.example.com" {
		t.Errorf("host = %q", cfg.Defaults.Host)
	}

	// Cached: later file changes are not observed within one invocation.
	if err := os.WriteFile(path, []byte("broken: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := f.Config(); err != nil || again != cfg {
		t.Errorf("config not cached: %v", err)
	}
}

func TestConfigInvalidIsClassified(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("version: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFactory(map[string]string{"FJO_CONFIG_DIR": dir})
	_, err := f.Config()
	if fjoerr.KindOf(err) != fjoerr.KindConfig {
		t.Fatalf("kind = %q, err = %v", fjoerr.KindOf(err), err)
	}
	if !strings.Contains(fjoerr.HintOf(err), "config.yaml") {
		t.Errorf("hint should name the file: %q", fjoerr.HintOf(err))
	}
}

func TestTokenRegisteredAsSecret(t *testing.T) {
	f := newFactory(map[string]string{"FJO_TOKEN": "opaque-token-value"})
	if strings.Contains(f.Redactor.String("x opaque-token-value y"), "opaque-token-value") {
		t.Error("FJO_TOKEN must be redacted")
	}
}

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// FileName is the configuration file name inside the config directory.
const FileName = "config.yaml"

// Dir returns the fjo configuration directory.
//
// Precedence: FJO_CONFIG_DIR > XDG_CONFIG_HOME/fjo > os.UserConfigDir()/fjo.
func Dir(getenv func(string) string) (string, error) {
	if d := getenv("FJO_CONFIG_DIR"); d != "" {
		return d, nil
	}
	if d := getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "fjo"), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config directory: %w", err)
	}
	return filepath.Join(d, "fjo"), nil
}

// Load reads the configuration from path. A missing file yields an empty
// default configuration; any other read, parse or validation error is returned.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg, err := Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes and validates configuration YAML. Unknown keys are rejected so
// typos do not silently change behavior.
func Parse(r io.Reader) (*Config, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	cfg := &Config{}
	if err := dec.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return New(), nil
		}
		return nil, fmt.Errorf("parse: %w", err)
	}
	if cfg.Hosts == nil {
		cfg.Hosts = map[string]*Host{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save validates cfg and writes it to path atomically: the data goes to a
// temporary file in the same directory which then replaces the target.
// The directory is created with 0700 and the file with 0600 permissions.
func Save(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("refusing to save invalid config: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if err := tmp.Chmod(0o600); err != nil && !isChmodUnsupported(err) {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("set config permissions: %w", err)
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// isChmodUnsupported reports errors from platforms (Windows) where file mode
// bits are not supported for this operation.
func isChmodUnsupported(err error) bool {
	return errors.Is(err, errors.ErrUnsupported)
}

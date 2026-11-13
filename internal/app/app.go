// Package app wires dependencies shared by all commands (the gh
// cmdutil.Factory pattern). Commands receive a *Factory and never construct
// infrastructure themselves, which keeps them thin and testable.
package app

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/credentials"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
	"github.com/maymeow/fjo/internal/logging"
	"github.com/maymeow/fjo/internal/prompt"
)

// Factory provides lazily-initialized dependencies to commands.
type Factory struct {
	IO        *iostreams.IOStreams
	Getenv    func(string) string
	BuildInfo build.Info
	Redactor  *logging.Redactor

	// Logger is replaced by the root command once --verbose/--debug are known.
	Logger *slog.Logger

	// Browser opens URLs (--web). Prompter asks interactive questions.
	Browser  browser.Opener
	Prompter prompt.Prompter
	// HTTPTransport overrides the base HTTP transport (tests).
	HTTPTransport http.RoundTripper
	// Credentials overrides the credential manager (tests); nil means the
	// OS keyring plus the opt-in file store in the config directory.
	Credentials *credentials.Manager
	// GitDir is the directory git commands run in; "" means the current
	// working directory (tests point it at a temporary repository).
	GitDir string

	configOnce sync.Once
	config     *config.Config
	configErr  error
	configPath string
}

// New builds a factory around the given streams and environment lookup.
func New(io *iostreams.IOStreams, getenv func(string) string, info build.Info) *Factory {
	r := logging.NewRedactor()
	// Register env-provided secrets immediately so nothing can log them.
	r.Add(getenv("FJO_TOKEN"))
	return &Factory{
		IO:        io,
		Getenv:    getenv,
		BuildInfo: info,
		Redactor:  r,
		Logger:    logging.New(io.ErrOut, logging.Quiet, r),
		Browser:   browser.New(getenv),
		Prompter:  prompt.New(io),
	}
}

// ConfigPath returns the path of the user configuration file.
func (f *Factory) ConfigPath() (string, error) {
	if f.configPath != "" {
		return f.configPath, nil
	}
	dir, err := config.Dir(f.Getenv)
	if err != nil {
		return "", fjoerr.Wrap(fjoerr.KindConfig, err, "locate configuration")
	}
	f.configPath = filepath.Join(dir, config.FileName)
	return f.configPath, nil
}

// Config loads the user configuration once and caches the result.
func (f *Factory) Config() (*config.Config, error) {
	f.configOnce.Do(func() {
		path, err := f.ConfigPath()
		if err != nil {
			f.configErr = err
			return
		}
		cfg, err := config.Load(path)
		if err != nil {
			f.configErr = fjoerr.Wrap(fjoerr.KindConfig, err, "invalid configuration").
				WithHint("fix or remove " + path)
			return
		}
		f.config = cfg
	})
	return f.config, f.configErr
}

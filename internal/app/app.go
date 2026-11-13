// Package app wires dependencies shared by all commands (the gh
// cmdutil.Factory pattern). Commands receive a *Factory and never construct
// infrastructure themselves, which keeps them thin and testable.
package app

import (
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/maymeow/fjo/internal/build"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
	"github.com/maymeow/fjo/internal/logging"
)

// Factory provides lazily-initialized dependencies to commands.
type Factory struct {
	IO        *iostreams.IOStreams
	Getenv    func(string) string
	BuildInfo build.Info
	Redactor  *logging.Redactor

	// Logger is replaced by the root command once --verbose/--debug are known.
	Logger *slog.Logger

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

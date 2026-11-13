package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/credentials"
	"github.com/maymeow/fjo/internal/fjoerr"
)

// SaveConfig persists cfg to the user configuration file.
func (f *Factory) SaveConfig(cfg *config.Config) error {
	path, err := f.ConfigPath()
	if err != nil {
		return err
	}
	if err := config.Save(path, cfg); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot save configuration")
	}
	f.config = cfg
	return nil
}

// CredentialStore returns the credential manager.
func (f *Factory) CredentialStore() (*credentials.Manager, error) {
	if f.Credentials != nil {
		return f.Credentials, nil
	}
	path, err := f.ConfigPath()
	if err != nil {
		return nil, err
	}
	file := filepath.Join(filepath.Dir(path), credentials.FileName)
	f.Credentials = &credentials.Manager{
		Keyring:  credentials.NewKeyring(),
		File:     &credentials.File{Path: file},
		FilePath: file,
	}
	return f.Credentials, nil
}

// ResolveHost picks the host ID for a command (B-12): the --hostname flag,
// then FJO_HOST, then defaults.host, then the only configured host.
func (f *Factory) ResolveHost(flag string) (string, error) {
	cfg, err := f.Config()
	if err != nil {
		return "", err
	}
	v := config.Resolve(
		config.Layer{Source: config.SourceFlag, Value: flag},
		config.Layer{Source: config.SourceEnv, Value: f.Getenv("FJO_HOST")},
		config.Layer{Source: config.SourceUser, Value: cfg.Defaults.Host},
	)
	if v.Value == "" {
		if ids := cfg.HostIDs(); len(ids) == 1 {
			return ids[0], nil
		}
		return "", fjoerr.Usage("no Forgejo host selected").
			WithHint("pass --hostname, set FJO_HOST, or log in with 'fjo auth login --hostname <host>'")
	}
	id, err := config.NormalizeHost(v.Value)
	if err != nil {
		return "", fjoerr.Usage("invalid host from %s: %v", v.Source, err)
	}
	return id, nil
}

// Account is the identity used for API calls to one host.
type Account struct {
	Host   string
	User   string // empty when the token comes from FJO_TOKEN
	Token  string
	Source credentials.Source
}

// AccountFor returns the credentials for host: FJO_TOKEN when set (B-15:
// it applies to whichever host the command targets and is never stored),
// otherwise the stored token of the host's active user.
func (f *Factory) AccountFor(host string) (*Account, error) {
	if tok := f.Getenv("FJO_TOKEN"); tok != "" {
		return &Account{Host: host, Token: tok, Source: credentials.SourceEnv}, nil
	}
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	h := cfg.Host(host)
	if h == nil || h.ActiveUser == "" {
		return nil, fjoerr.New(fjoerr.KindAuth, "not logged in to %s", host).
			WithHint("run 'fjo auth login --hostname " + host + "' or set FJO_TOKEN")
	}
	return f.StoredAccount(host, h.ActiveUser)
}

// StoredAccount loads the stored token of a specific user.
func (f *Factory) StoredAccount(host, user string) (*Account, error) {
	store, err := f.CredentialStore()
	if err != nil {
		return nil, err
	}
	tok, src, err := store.Get(host, user)
	switch {
	case errors.Is(err, credentials.ErrNotFound):
		return nil, fjoerr.New(fjoerr.KindAuth, "no stored token for %s on %s", user, host).
			WithHint("run 'fjo auth login --hostname " + host + "'")
	case err != nil:
		return nil, fjoerr.Wrap(fjoerr.KindAuth, err, "cannot read token for %s on %s", user, host).
			WithHint("set FJO_TOKEN, or log in again with --insecure-storage if no OS credential store is available")
	}
	f.Redactor.Add(tok)
	return &Account{Host: host, User: user, Token: tok, Source: src}, nil
}

// APIClient builds a client for a configured (or new, h == nil) host.
func (f *Factory) APIClient(host string, h *config.Host, token string) (*api.Client, error) {
	f.Redactor.Add(token)
	caFile := ""
	if h != nil {
		caFile = h.CAFile
	}
	c, err := api.New(api.Options{
		BaseURL:   config.APIURL(host, h),
		Token:     token,
		UserAgent: fmt.Sprintf("fjo/%s (%s)", f.BuildInfo.Version, f.BuildInfo.Platform),
		CAFile:    caFile,
		Logger:    f.Logger,
		Transport: f.HTTPTransport,
	})
	if err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindConfig, err, "cannot create API client for %s", host)
	}
	return c, nil
}

// ClientFor resolves credentials and builds an authenticated client.
func (f *Factory) ClientFor(host string) (*api.Client, *Account, error) {
	acct, err := f.AccountFor(host)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := f.Config()
	if err != nil {
		return nil, nil, err
	}
	c, err := f.APIClient(host, cfg.Host(host), acct.Token)
	return c, acct, err
}

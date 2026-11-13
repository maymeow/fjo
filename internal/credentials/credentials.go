// Package credentials stores access tokens per (host ID, username)
// (IMPLEMENTATION §7.2, §7.5, BACKLOG B-5).
//
// Tokens live in the OS credential store (Windows Credential Manager, macOS
// Keychain, Linux Secret Service). Plain-text file storage is used only when
// the user explicitly asks for it with `fjo auth login --insecure-storage`;
// there is no silent fallback.
package credentials

import (
	"errors"
	"fmt"
)

// Source tells where a token came from.
type Source string

// Token sources.
const (
	SourceKeyring Source = "keyring"
	SourceFile    Source = "insecure storage"
	SourceEnv     Source = "FJO_TOKEN"
)

// ErrNotFound means no token is stored for the account.
var ErrNotFound = errors.New("no stored token")

// Store is a backend holding one token per (host, user).
type Store interface {
	Get(host, user string) (string, error)
	Set(host, user, token string) error
	Delete(host, user string) error
}

// Manager combines the secure keyring with the opt-in file store.
type Manager struct {
	Keyring Store
	File    Store
	// FilePath is shown to the user when the file store is used.
	FilePath string
}

// Get returns the token for an account, preferring the keyring. A keyring
// failure is reported only when the file store has no token either.
func (m *Manager) Get(host, user string) (string, Source, error) {
	tok, kerr := m.Keyring.Get(host, user)
	if kerr == nil {
		return tok, SourceKeyring, nil
	}
	if m.File != nil {
		tok, ferr := m.File.Get(host, user)
		if ferr == nil {
			return tok, SourceFile, nil
		}
		if !errors.Is(ferr, ErrNotFound) {
			return "", "", ferr
		}
	}
	if errors.Is(kerr, ErrNotFound) {
		return "", "", ErrNotFound
	}
	return "", "", fmt.Errorf("read OS credential store: %w", kerr)
}

// Set stores a token. With insecure=false it goes to the keyring and a
// keyring failure is returned as an error; with insecure=true it goes to the
// file store. The copy in the other backend is removed so only one exists.
func (m *Manager) Set(host, user, token string, insecure bool) (Source, error) {
	if insecure {
		if m.File == nil {
			return "", errors.New("insecure storage is not available")
		}
		if err := m.File.Set(host, user, token); err != nil {
			return "", err
		}
		_ = m.Keyring.Delete(host, user)
		return SourceFile, nil
	}
	if err := m.Keyring.Set(host, user, token); err != nil {
		return "", fmt.Errorf("store token in OS credential store: %w", err)
	}
	if m.File != nil {
		_ = m.File.Delete(host, user)
	}
	return SourceKeyring, nil
}

// Delete removes the token from both backends. Missing tokens are not an
// error; a keyring failure is returned only if the token could not be
// removed anywhere.
func (m *Manager) Delete(host, user string) error {
	kerr := m.Keyring.Delete(host, user)
	var ferr error
	if m.File != nil {
		ferr = m.File.Delete(host, user)
	}
	if kerr != nil && !errors.Is(kerr, ErrNotFound) {
		return fmt.Errorf("remove token from OS credential store: %w", kerr)
	}
	if ferr != nil && !errors.Is(ferr, ErrNotFound) {
		return ferr
	}
	return nil
}

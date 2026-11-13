package credentials

import (
	"errors"
	"time"

	"github.com/zalando/go-keyring"
)

// Keyring stores tokens in the OS credential store. The service name is
// "fjo:<host ID>" and the account is the server username (like gh's
// "gh:<host>").
type Keyring struct {
	// Timeout bounds every keyring call; Secret Service over D-Bus can hang
	// when no session bus or unlock prompt is available.
	Timeout time.Duration
}

// NewKeyring returns a keyring store with a 3 s timeout.
func NewKeyring() *Keyring { return &Keyring{Timeout: 3 * time.Second} }

func service(host string) string { return "fjo:" + host }

var errTimeout = errors.New("timed out waiting for the OS credential store")

func (k *Keyring) run(fn func() (string, error)) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := fn()
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		if errors.Is(r.err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return r.s, r.err
	case <-time.After(k.Timeout):
		return "", errTimeout
	}
}

// Get implements Store.
func (k *Keyring) Get(host, user string) (string, error) {
	return k.run(func() (string, error) { return keyring.Get(service(host), user) })
}

// Set implements Store.
func (k *Keyring) Set(host, user, token string) error {
	_, err := k.run(func() (string, error) { return "", keyring.Set(service(host), user, token) })
	return err
}

// Delete implements Store.
func (k *Keyring) Delete(host, user string) error {
	_, err := k.run(func() (string, error) { return "", keyring.Delete(service(host), user) })
	return err
}

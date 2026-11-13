package credentials

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/maymeow/fjo/internal/fsutil"
)

// FileName is the plain-text credentials file used by --insecure-storage.
// It is separate from config.yaml so the config never contains secrets.
const FileName = "credentials.yaml"

const fileHeader = "# fjo credentials stored with --insecure-storage. Contains secrets in plain text.\n" +
	"# Prefer the OS credential store: fjo auth login (without --insecure-storage).\n"

type fileData struct {
	Hosts map[string]map[string]string `yaml:"hosts"`
}

// File is the opt-in plain-text token store (0600 file).
type File struct {
	Path string
	mu   sync.Mutex
}

func (f *File) load() (*fileData, error) {
	d := &fileData{Hosts: map[string]map[string]string{}}
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(d); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: %w", f.Path, err)
	}
	if d.Hosts == nil {
		d.Hosts = map[string]map[string]string{}
	}
	return d, nil
}

func (f *File) save(d *fileData) error {
	for h, users := range d.Hosts {
		if len(users) == 0 {
			delete(d.Hosts, h)
		}
	}
	if len(d.Hosts) == 0 {
		err := os.Remove(f.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var buf bytes.Buffer
	buf.WriteString(fileHeader)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(f.Path, buf.Bytes())
}

// Get implements Store.
func (f *File) Get(host, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, err := f.load()
	if err != nil {
		return "", err
	}
	tok, ok := d.Hosts[host][user]
	if !ok || tok == "" {
		return "", ErrNotFound
	}
	return tok, nil
}

// Set implements Store.
func (f *File) Set(host, user, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, err := f.load()
	if err != nil {
		return err
	}
	if d.Hosts[host] == nil {
		d.Hosts[host] = map[string]string{}
	}
	d.Hosts[host][user] = token
	return f.save(d)
}

// Delete implements Store.
func (f *File) Delete(host, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := d.Hosts[host][user]; !ok {
		return ErrNotFound
	}
	delete(d.Hosts[host], user)
	return f.save(d)
}

// Memory is an in-memory Store for tests. Fail makes every call return it.
type Memory struct {
	mu     sync.Mutex
	tokens map[string]string
	Fail   error
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{tokens: map[string]string{}} }

func key(host, user string) string { return host + "\x00" + user }

// Get implements Store.
func (m *Memory) Get(host, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return "", m.Fail
	}
	t, ok := m.tokens[key(host, user)]
	if !ok {
		return "", ErrNotFound
	}
	return t, nil
}

// Set implements Store.
func (m *Memory) Set(host, user, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	m.tokens[key(host, user)] = token
	return nil
}

// Delete implements Store.
func (m *Memory) Delete(host, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	if _, ok := m.tokens[key(host, user)]; !ok {
		return ErrNotFound
	}
	delete(m.tokens, key(host, user))
	return nil
}

// Keys lists stored "host/user" pairs (tests).
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.tokens {
		out = append(out, string(bytes.ReplaceAll([]byte(k), []byte("\x00"), []byte("/"))))
	}
	sort.Strings(out)
	return out
}

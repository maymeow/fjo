package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestFileStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", FileName)
	f := &File{Path: path}

	if _, err := f.Get("h", "u"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty Get err = %v", err)
	}
	if err := f.Set("git.example.com:3000", "alice", "t1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("git.example.com:3000", "bob", "t2"); err != nil {
		t.Fatal(err)
	}
	if tok, err := f.Get("git.example.com:3000", "bob"); err != nil || tok != "t2" {
		t.Fatalf("Get = %q, %v", tok, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# fjo credentials") {
		t.Errorf("missing warning header:\n%s", data)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", st.Mode().Perm())
		}
	}

	if err := f.Delete("git.example.com:3000", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("git.example.com:3000", "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete err = %v", err)
	}
	if err := f.Delete("git.example.com:3000", "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("empty credentials file should be removed")
	}
}

func TestFileStoreRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	_ = os.WriteFile(path, []byte("unknown: 1\n"), 0o600)
	if _, err := (&File{Path: path}).Get("h", "u"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestManagerPrefersKeyringAndKeepsOneCopy(t *testing.T) {
	k, f := NewMemory(), NewMemory()
	m := &Manager{Keyring: k, File: f}

	if src, err := m.Set("h", "u", "file-token", true); err != nil || src != SourceFile {
		t.Fatalf("insecure Set = %v, %v", src, err)
	}
	if tok, src, err := m.Get("h", "u"); err != nil || tok != "file-token" || src != SourceFile {
		t.Fatalf("Get = %q %q %v", tok, src, err)
	}
	if src, err := m.Set("h", "u", "kr-token", false); err != nil || src != SourceKeyring {
		t.Fatalf("Set = %v, %v", src, err)
	}
	if len(f.Keys()) != 0 {
		t.Errorf("file copy not removed: %v", f.Keys())
	}
	if tok, src, _ := m.Get("h", "u"); tok != "kr-token" || src != SourceKeyring {
		t.Errorf("Get = %q %q", tok, src)
	}
	if err := m.Delete("h", "u"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Get("h", "u"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after Delete err = %v", err)
	}
	if err := m.Delete("h", "u"); err != nil {
		t.Errorf("deleting a missing token must succeed: %v", err)
	}
}

func TestManagerNoSilentFallback(t *testing.T) {
	k, f := NewMemory(), NewMemory()
	k.Fail = errors.New("no secret service")
	m := &Manager{Keyring: k, File: f}

	if _, err := m.Set("h", "u", "t", false); err == nil {
		t.Fatal("keyring failure must not fall back to the file")
	}
	if len(f.Keys()) != 0 {
		t.Fatal("token written to file without opt-in")
	}
	if _, _, err := m.Get("h", "u"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("keyring failure should be reported: %v", err)
	}
	// Explicit opt-in still works, and Get finds it despite the keyring error.
	if _, err := m.Set("h", "u", "t", true); err != nil {
		t.Fatal(err)
	}
	if tok, src, err := m.Get("h", "u"); err != nil || tok != "t" || src != SourceFile {
		t.Errorf("Get = %q %q %v", tok, src, err)
	}
	if err := m.Delete("h", "u"); err == nil {
		t.Error("Delete should report the keyring failure")
	}
}

func TestKeyringWithMock(t *testing.T) {
	keyring.MockInit()
	k := NewKeyring()
	if _, err := k.Get("h", "u"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := k.Set("h", "u", "tok"); err != nil {
		t.Fatal(err)
	}
	if tok, err := k.Get("h", "u"); err != nil || tok != "tok" {
		t.Fatalf("Get = %q, %v", tok, err)
	}
	if err := k.Delete("h", "u"); err != nil {
		t.Fatal(err)
	}
}

func TestKeyringTimeout(t *testing.T) {
	k := &Keyring{Timeout: 10 * time.Millisecond}
	_, err := k.run(func() (string, error) { time.Sleep(time.Second); return "", nil })
	if !errors.Is(err, errTimeout) {
		t.Errorf("err = %v", err)
	}
}

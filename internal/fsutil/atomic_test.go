package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomicNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	want := []byte("hello, world\n")

	if err := WriteFileAtomic(path, want); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestWriteFileAtomicOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")

	if err := WriteFileAtomic(path, []byte("first")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second")); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
}

func TestWriteFileAtomicCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "file.txt")

	if err := WriteFileAtomic(path, []byte("data")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	st, err := os.Stat(filepath.Join(dir, "a", "b", "c"))
	if err != nil {
		t.Fatalf("Stat directory: %v", err)
	}
	if !st.IsDir() {
		t.Errorf("%s is not a directory", filepath.Join(dir, "a", "b", "c"))
	}
}

func TestWriteFileAtomicPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.txt")

	if err := WriteFileAtomic(path, []byte("data")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want %o", got, 0o600)
	}

	st, err = os.Stat(filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatalf("Stat directory: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode = %o, want %o", got, 0o700)
	}
}

func TestWriteFileAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	if err := WriteFileAtomic(path, []byte("data")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("directory contains %v, want only file.txt", names)
	}
	if got := entries[0].Name(); got != "file.txt" {
		t.Errorf("directory entry = %q, want %q", got, "file.txt")
	}
}

func TestWriteFileAtomicError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := WriteFileAtomic(filepath.Join(file, "x.txt"), []byte("data")); err == nil {
		t.Fatal("WriteFileAtomic with a file as parent directory should fail")
	}
}

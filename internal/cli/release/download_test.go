package release_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
)

type fakeAsset struct {
	name, content, url string
	size               int64
}

// downloadServer serves release v1 with the given assets. Asset URLs that
// are empty point to /files/<name> on the same server.
func downloadServer(t *testing.T, assets []fakeAsset, files http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == endpoint+"/tags/v1" || r.URL.Path == endpoint+"/latest"):
			base := "https://" + r.Host
			list := make([]map[string]any, 0, len(assets))
			for _, a := range assets {
				link := a.url
				if link == "" {
					link = base + "/files/" + a.name
				}
				size := a.size
				if size == 0 {
					size = int64(len(a.content))
				}
				list = append(list, map[string]any{"id": 1, "name": a.name, "size": size, "browser_download_url": link, "type": "attachment"})
			}
			if err := json.NewEncoder(w).Encode(map[string]any{
				"id": 9, "tag_name": "v1", "assets": list,
				"tarball_url": base + "/org/project/archive/v1.tar.gz", "zipball_url": base + "/org/project/archive/v1.zip",
			}); err != nil {
				t.Error(err)
			}
		case strings.HasPrefix(r.URL.Path, "/files/"):
			if r.Header.Get("Authorization") != "token test-token" {
				t.Errorf("in-scope download without token: %q", r.Header.Get("Authorization"))
			}
			name := strings.TrimPrefix(r.URL.Path, "/files/")
			for _, a := range assets {
				if a.name == name {
					io.WriteString(w, a.content)
					return
				}
			}
			http.NotFound(w, r)
		default:
			files(w, r)
		}
	}
}

func unexpected(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}
}

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReleaseDownloadVerifiesChecksums(t *testing.T) {
	manifest := sha("alpha") + "  app.bin\n" + "SHA256 (notes.txt) = " + sha("notes") + "\n"
	assets := []fakeAsset{{name: "app.bin", content: "alpha"}, {name: "notes.txt", content: "notes"}, {name: "fjo_1.0_checksums.txt", content: manifest}}
	dir := t.TempDir()
	got := run(t, downloadServer(t, assets, unexpected(t)), false, "download", "-D", dir)
	if got.code != 0 || got.stdout != "" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
	if names := dirEntries(t, dir); !slices.Equal(names, []string{"app.bin", "fjo_1.0_checksums.txt", "notes.txt"}) {
		t.Fatalf("files %v", names)
	}
	if readFile(t, filepath.Join(dir, "app.bin")) != "alpha" {
		t.Fatal("app.bin content differs")
	}

	assets[0].content = "omega"
	dir = t.TempDir()
	got = run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-D", dir, "-p", "*.bin")
	if got.code != fjoerr.ExitError || !strings.Contains(got.stderr, "SHA-256 checksum of app.bin does not match") || len(dirEntries(t, dir)) != 0 {
		t.Fatalf("mismatch: %+v, files %v", got, dirEntries(t, dir))
	}
	got = run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-D", dir, "-p", "*.bin", "--skip-verify")
	if got.code != 0 || readFile(t, filepath.Join(dir, "app.bin")) != "omega" {
		t.Fatalf("--skip-verify: %+v", got)
	}
}

func TestReleaseDownloadProgressOnTerminal(t *testing.T) {
	assets := []fakeAsset{{name: "app.bin", content: "alpha"}, {name: "checksums.txt", content: sha("alpha") + " *app.bin\n"}}
	got := runWith(t, downloadServer(t, assets, unexpected(t)), true, "", "", "download", "v1", "-D", t.TempDir(), "-p", "app.*")
	if got.code != 0 || !strings.Contains(got.stderr, "Downloaded app.bin (5 B, SHA-256 verified)") {
		t.Fatalf("%+v", got)
	}
}

func TestReleaseDownloadSizeMismatchLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	assets := []fakeAsset{{name: "app.bin", content: "short", size: 10}}
	got := run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-D", dir)
	if got.code != fjoerr.ExitError || !strings.Contains(got.stderr, "got 5 of 10 bytes") || len(dirEntries(t, dir)) != 0 {
		t.Fatalf("%+v, files %v", got, dirEntries(t, dir))
	}
}

func TestReleaseDownloadToStdout(t *testing.T) {
	assets := []fakeAsset{{name: "app.bin", content: "alpha"}, {name: "app.sig", content: "sig"}}
	got := run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-p", "*.bin", "-O", "-")
	if got.code != 0 || got.stdout != "alpha" {
		t.Fatalf("%+v", got)
	}
	got = run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-p", "app.*", "-O", "-")
	if got.code != fjoerr.ExitUsage {
		t.Fatalf("several matches with --output: %+v", got)
	}
	out := filepath.Join(t.TempDir(), "renamed.bin")
	got = run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-p", "*.bin", "-O", out)
	if got.code != 0 || readFile(t, out) != "alpha" {
		t.Fatalf("--output file: %+v", got)
	}
}

func TestReleaseDownloadExistingFiles(t *testing.T) {
	assets := []fakeAsset{{name: "app.bin", content: "new"}}
	for _, tc := range []struct {
		flag, want string
		code       fjoerr.ExitCode
	}{
		{want: "old", code: fjoerr.ExitConflict},
		{flag: "--skip-existing", want: "old"},
		{flag: "--clobber", want: "new"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "app.bin")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		args := []string{"download", "v1", "-D", dir}
		if tc.flag != "" {
			args = append(args, tc.flag)
		}
		got := run(t, downloadServer(t, assets, unexpected(t)), false, args...)
		if got.code != tc.code || readFile(t, path) != tc.want || len(dirEntries(t, dir)) != 1 {
			t.Fatalf("%q: %+v, files %v", tc.flag, got, dirEntries(t, dir))
		}
	}
}

func TestReleaseDownloadRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"../escape.bin", "nested/file.bin", `back\slash.bin`, ".."} {
		parent := t.TempDir()
		dir := filepath.Join(parent, "out")
		assets := []fakeAsset{{name: name, content: "x", url: "https://forge.invalid/x"}}
		got := run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-D", dir)
		if got.code != fjoerr.ExitError || !strings.Contains(got.stderr, "unsafe name") {
			t.Fatalf("%q: %+v", name, got)
		}
		if names := dirEntries(t, parent); len(names) != 0 {
			t.Fatalf("%q: wrote %v", name, names)
		}
	}
}

func TestReleaseDownloadDoesNotLeakTokenToOtherHosts(t *testing.T) {
	var headers []string
	external := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("Authorization"))
		io.WriteString(w, "external")
	}))
	t.Cleanup(external.Close)
	assets := []fakeAsset{
		{name: "direct.bin", content: "external", url: external.URL + "/direct.bin"},
		{name: "redirected.bin", content: "external"},
	}
	dir := t.TempDir()
	got := run(t, downloadServer(t, assets, unexpected(t)), false, "download", "v1", "-D", dir, "-p", "direct.bin")
	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	assets[1].url = "REDIRECT"
	handler := downloadServer(t, assets, unexpected(t))
	got = run(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			if r.Header.Get("Authorization") == "" {
				t.Error("in-scope redirect source without token")
			}
			http.Redirect(w, r, external.URL+"/redirected.bin", http.StatusFound)
			return
		}
		rec := httptest.NewRecorder()
		handler(rec, r)
		body := strings.ReplaceAll(rec.Body.String(), `"REDIRECT"`, `"https://`+r.Host+`/redirect"`)
		w.WriteHeader(rec.Code)
		io.WriteString(w, body)
	}, false, "download", "v1", "-D", dir, "-p", "redirected.bin")
	if got.code != 0 || readFile(t, filepath.Join(dir, "redirected.bin")) != "external" {
		t.Fatalf("%+v", got)
	}
	if len(headers) != 2 || headers[0] != "" || headers[1] != "" {
		t.Fatalf("Authorization sent to another host: %q", headers)
	}
}

func TestReleaseDownloadInterruptRemovesPartialFile(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dir := t.TempDir()
	assets := []fakeAsset{{name: "big.bin", url: "SLOW", size: 1 << 20}}
	handler := downloadServer(t, assets, unexpected(t))
	got := runContext(ctx, t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			w.Header().Set("Content-Length", "1048576")
			io.WriteString(w, strings.Repeat("x", 4096))
			w.(http.Flusher).Flush()
			cancel()
			<-r.Context().Done()
			return
		}
		rec := httptest.NewRecorder()
		handler(rec, r)
		io.WriteString(w, strings.ReplaceAll(rec.Body.String(), `"SLOW"`, `"https://`+r.Host+`/slow"`))
	}, false, "", "", "download", "v1", "-D", dir)
	if got.code != fjoerr.ExitInterrupted || len(dirEntries(t, dir)) != 0 {
		t.Fatalf("%+v, files %v", got, dirEntries(t, dir))
	}
}

func TestReleaseDownloadArchive(t *testing.T) {
	dir := t.TempDir()
	got := run(t, downloadServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/project/archive/v1.tar.gz" {
			unexpected(t)(w, r)
			return
		}
		io.WriteString(w, "tarball")
	}), false, "download", "v1", "-A", "tar.gz", "-D", dir)
	if got.code != 0 || readFile(t, filepath.Join(dir, "project-v1.tar.gz")) != "tarball" {
		t.Fatalf("%+v, files %v", got, dirEntries(t, dir))
	}
}

func TestReleaseDownloadUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--clobber", "--skip-existing"},
		{"-A", "rar"},
		{"-A", "zip", "-p", "*"},
		{"-O", "out", "-D", "dir"},
		{"-p", "["},
		{"../v1"},
	} {
		got := run(t, unexpected(t), false, append([]string{"download"}, args...)...)
		if got.code != fjoerr.ExitUsage {
			t.Fatalf("%v: %+v", args, got)
		}
	}
	got := run(t, downloadServer(t, nil, unexpected(t)), false, "download", "v1", "-D", t.TempDir())
	if got.code != fjoerr.ExitNotFound || !strings.Contains(got.stderr, "has no assets") {
		t.Fatalf("no assets: %+v", got)
	}
}

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}

func TestReleaseDownloadStreamsWithoutBuffering(t *testing.T) {
	const size = 64 << 20
	dir := t.TempDir()
	assets := []fakeAsset{{name: "big.bin", url: "BIG", size: size}}
	handler := downloadServer(t, assets, unexpected(t))
	serve := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			w.Header().Set("Content-Length", strconv.Itoa(size))
			if _, err := io.CopyN(w, zeroReader{}, size); err != nil {
				t.Error(err)
			}
			return
		}
		rec := httptest.NewRecorder()
		handler(rec, r)
		io.WriteString(w, strings.ReplaceAll(rec.Body.String(), `"BIG"`, `"https://`+r.Host+`/big"`))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := run(t, serve, false, "download", "v1", "-D", dir)
	runtime.ReadMemStats(&after)
	info, err := os.Stat(filepath.Join(dir, "big.bin"))
	if got.code != 0 || err != nil || info.Size() != size {
		t.Fatalf("%+v, %v", got, err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/4 {
		t.Fatalf("download allocated %d bytes for a %d byte file", allocated, size)
	}
}

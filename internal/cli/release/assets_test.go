package release_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
)

const assetsEndpoint = endpoint + "/9/assets"

const assetReleaseJSON = `{"id":9,"tag_name":"v1","name":"One","draft":false,"html_url":"https://forge.example/org/project/releases/tag/v1",
	"assets":[{"id":31,"name":"existing.zip","size":3}]}`

const enabledSettings = `{"enabled":true,"allowed_types":"","max_size":2048,"max_files":5}`

func writeAsset(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// assetServer serves the release lookup and attachment settings and hands
// every other request to next.
func assetServer(settings string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == endpoint+"/tags/v1":
			io.WriteString(w, assetReleaseJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/settings/attachment":
			io.WriteString(w, settings)
		default:
			next(w, r)
		}
	}
}

func TestReleaseUploadStreamsWithoutBuffering(t *testing.T) {
	const size = 64 << 20
	path := filepath.Join(t.TempDir(), "big.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var received int64
	handler := assetServer(enabledSettings, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != assetsEndpoint {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("name") != "big.bin" || r.Header.Get("Content-Type") != "application/octet-stream" ||
			r.ContentLength != size {
			t.Errorf("unexpected upload: name %q, type %q, length %d", r.URL.Query().Get("name"), r.Header.Get("Content-Type"), r.ContentLength)
		}
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			t.Error(err)
		}
		received = n
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":40,"name":"big.bin","size":`+strconv.Itoa(size)+`}`)
	})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := run(t, handler, false, "upload", "v1", path)
	runtime.ReadMemStats(&after)
	if got.code != 0 || got.stdout != "" || got.stderr != "" || received != size {
		t.Fatalf("received %d bytes: %+v", received, got)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/4 {
		t.Fatalf("upload allocated %d bytes for a %d byte file", allocated, size)
	}
}

func TestReleaseUploadShowsProgressOnTerminal(t *testing.T) {
	path := writeAsset(t, "app.zip", "content")
	got := runWith(t, assetServer(enabledSettings, func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":40,"name":"app.zip","size":7}`)
	}), true, "", "", "upload", "v1", path)
	if got.code != 0 || !strings.Contains(got.stderr, "Uploading app.zip 100%") || !strings.Contains(got.stderr, "Uploaded app.zip (7 B)") {
		t.Fatalf("%+v", got)
	}
}

func TestReleaseUploadRefusalsBeforeTransfer(t *testing.T) {
	existing := writeAsset(t, "existing.zip", "new")
	fresh := writeAsset(t, "fresh.zip", "new")
	for _, tc := range []struct {
		name, settings, text string
		args                 []string
		want                 fjoerr.ExitCode
	}{
		{name: "disabled", settings: `{"enabled":false}`, args: []string{fresh}, want: fjoerr.ExitUnsupported, text: "attachments are disabled"},
		{name: "existing", settings: enabledSettings, args: []string{fresh, existing}, want: fjoerr.ExitConflict, text: "--clobber"},
		{name: "missing-file", settings: enabledSettings, args: []string{filepath.Join(t.TempDir(), "absent")}, want: fjoerr.ExitUsage},
		{name: "directory", settings: enabledSettings, args: []string{t.TempDir()}, want: fjoerr.ExitUsage},
		{name: "same-name", settings: enabledSettings, args: []string{fresh, fresh}, want: fjoerr.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, assetServer(tc.settings, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL)
				http.NotFound(w, r)
			}), false, append([]string{"upload", "v1"}, tc.args...)...)
			if got.code != tc.want || !strings.Contains(got.stderr, tc.text) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestReleaseUploadClobberReplacesAfterUpload(t *testing.T) {
	path := writeAsset(t, "existing.zip", "new")
	var calls []string
	var temporary string
	got := run(t, assetServer(enabledSettings, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == assetsEndpoint:
			temporary = r.URL.Query().Get("name")
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":40,"name":"`+temporary+`","size":3}`)
		case r.Method == http.MethodDelete && r.URL.Path == assetsEndpoint+"/31":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && r.URL.Path == assetsEndpoint+"/40":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["name"] != "existing.zip" || len(payload) != 1 {
				t.Errorf("unexpected rename %v %v", payload, err)
			}
			io.WriteString(w, `{"id":40,"name":"existing.zip","size":3}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}), false, "upload", "v1", path, "--clobber")
	want := []string{"POST " + assetsEndpoint, "DELETE " + assetsEndpoint + "/31", "PATCH " + assetsEndpoint + "/40"}
	if got.code != 0 || strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v: %+v", calls, got)
	}
	if !strings.HasPrefix(temporary, "fjo-upload-") || !strings.HasSuffix(temporary, "-existing.zip") {
		t.Fatalf("temporary name %q", temporary)
	}
}

func TestReleaseUploadFailures(t *testing.T) {
	first := writeAsset(t, "a.zip", "a")
	second := writeAsset(t, "b.zip", "b")
	third := writeAsset(t, "c.zip", "c")
	for _, tc := range []struct {
		name   string
		status int
		want   fjoerr.ExitCode
		text   string
	}{
		{name: "too-large", status: http.StatusRequestEntityTooLarge, want: fjoerr.ExitConflict, text: "size limit or storage quota"},
		{name: "server-error", status: http.StatusServiceUnavailable, want: fjoerr.ExitUnavailable, text: "uploaded a.zip; not uploaded b.zip, c.zip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			posts := 0
			got := run(t, assetServer(enabledSettings, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != assetsEndpoint {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				mu.Lock()
				posts++
				n := posts
				mu.Unlock()
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Error(err)
				}
				if n == 2 || tc.status == http.StatusRequestEntityTooLarge {
					w.WriteHeader(tc.status)
					return
				}
				w.WriteHeader(http.StatusCreated)
				io.WriteString(w, `{"id":40,"name":"a.zip","size":1}`)
			}), false, "upload", "v1", first, second, third)
			mu.Lock()
			defer mu.Unlock()
			if got.code != tc.want || !strings.Contains(got.stderr, tc.text) {
				t.Fatalf("%+v", got)
			}
			if tc.status == http.StatusServiceUnavailable && posts != 2 {
				t.Fatalf("upload was retried or continued: %d POST requests", posts)
			}
		})
	}
}

func TestReleaseCreateWithAssets(t *testing.T) {
	path := writeAsset(t, "app.zip", "zip")
	for _, tc := range []struct {
		name         string
		extra        []string
		uploadStatus int
		wantPatch    bool
		want         fjoerr.ExitCode
		text         string
	}{
		{name: "published", uploadStatus: http.StatusCreated, wantPatch: true},
		{name: "draft", extra: []string{"--draft"}, uploadStatus: http.StatusCreated},
		{name: "upload-fails", uploadStatus: http.StatusForbidden, want: fjoerr.ExitForbidden, text: "fjo release upload v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patched := false
			got := run(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/settings/attachment":
					io.WriteString(w, enabledSettings)
				case r.Method == http.MethodGet && r.URL.Path == endpoint:
					io.WriteString(w, "[]")
				case r.Method == http.MethodGet:
					http.NotFound(w, r)
				case r.Method == http.MethodPost && r.URL.Path == endpoint:
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["draft"] != true {
						t.Errorf("release not created as draft: %v %v", payload, err)
					}
					w.WriteHeader(http.StatusCreated)
					io.WriteString(w, strings.Replace(assetReleaseJSON, `"draft":false`, `"draft":true`, 1))
				case r.Method == http.MethodPost && r.URL.Path == assetsEndpoint:
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Error(err)
					}
					w.WriteHeader(tc.uploadStatus)
					io.WriteString(w, `{"id":40,"name":"app.zip","size":3}`)
				case r.Method == http.MethodPatch && r.URL.Path == endpoint+"/9" && tc.wantPatch:
					patched = true
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload) != 1 || payload["draft"] != false {
						t.Errorf("unexpected publish payload %v %v", payload, err)
					}
					io.WriteString(w, assetReleaseJSON)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}, false, append([]string{"create", "v1", path, "-t", "One"}, tc.extra...)...)
			if got.code != tc.want || patched != tc.wantPatch || !strings.Contains(got.stderr, tc.text) {
				t.Fatalf("patched %v: %+v", patched, got)
			}
		})
	}
}

func TestReleaseDeleteAsset(t *testing.T) {
	for _, tc := range []struct {
		name, input, asset string
		tty                bool
		want               fjoerr.ExitCode
	}{
		{name: "yes", asset: "existing.zip"},
		{name: "confirmed", tty: true, input: "existing.zip\n", asset: "existing.zip"},
		{name: "cancelled", tty: true, input: "no\n", asset: "existing.zip", want: fjoerr.ExitCancel},
		{name: "missing", asset: "absent.zip", want: fjoerr.ExitNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deleted := false
			args := []string{"delete-asset", "v1", tc.asset}
			if !tc.tty {
				args = append(args, "--yes")
			}
			got := runWith(t, assetServer(enabledSettings, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != assetsEndpoint+"/31" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				deleted = true
				w.WriteHeader(http.StatusNoContent)
			}), tc.tty, tc.input, "", args...)
			if got.code != tc.want || deleted != (tc.want == 0) {
				t.Fatalf("deleted %v: %+v", deleted, got)
			}
		})
	}
	got := run(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
	}, false, "delete-asset", "v1", "existing.zip")
	if got.code != fjoerr.ExitUsage {
		t.Fatalf("%+v", got)
	}
}

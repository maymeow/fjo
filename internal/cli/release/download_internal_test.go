package release

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestSafeAssetName(t *testing.T) {
	for _, tc := range []struct {
		name          string
		posix, window bool
	}{
		{"app.tar.gz", true, true},
		{".hidden", true, true},
		{"", false, false},
		{"..", false, false},
		{"../x", false, false},
		{`a\b`, false, false},
		{"tab\tname", false, false},
		{"CON", true, false},
		{"nul.txt", true, false},
		{"com1.log", true, false},
		{"LPT9", true, false},
		{"console.txt", true, true},
		{"com10", true, true},
		{"c:file", true, false},
		{"trailing.", true, false},
		{"trailing ", true, false},
		{"what?.txt", true, false},
	} {
		if got := safeAssetName(tc.name, false) == nil; got != tc.posix {
			t.Errorf("%q on POSIX: safe = %v", tc.name, got)
		}
		if got := safeAssetName(tc.name, true) == nil; got != tc.window {
			t.Errorf("%q on Windows: safe = %v", tc.name, got)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	a := strings.Repeat("ab", 32)
	b := strings.Repeat("cd", 32)
	manifest := "# comment\n" + a + "  app.tar.gz\r\n" + b + " *app.zip\n" + "SHA256 (app.deb) = " + a + "\n" +
		strings.Repeat("ef", 64) + "  sha512.bin\n" + "garbage\n"
	sums := map[string][]byte{}
	if err := parseChecksums([]byte(manifest), sums); err != nil {
		t.Fatal(err)
	}
	if len(sums) != 3 || hex.EncodeToString(sums["app.tar.gz"]) != a || hex.EncodeToString(sums["app.zip"]) != b || hex.EncodeToString(sums["app.deb"]) != a {
		t.Fatalf("parsed %v", sums)
	}
	if err := parseChecksums([]byte(b+"  app.tar.gz\n"), sums); err == nil {
		t.Fatal("conflicting digests accepted")
	}
}

func TestIsChecksumManifest(t *testing.T) {
	for name, want := range map[string]bool{
		"checksums.txt": true, "fjo_0.2.0_checksums.txt": true, "SHA256SUMS": true, "sha256sums.txt": true,
		"checksums.txt.sig": false, "app.tar.gz": false,
	} {
		if isChecksumManifest(name) != want {
			t.Errorf("%s: want %v", name, want)
		}
	}
}

package repo

import (
	"bytes"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/api"
)

func TestSanitizeRepoForTerminal(t *testing.T) {
	r := &api.Repository{
		FullName:    "org/project",
		Description: "line one\nline two \x1b]52;c;cHduZWQ=\x07",
		Topics:      []string{"go\x1b[31m"},
		Parent:      &api.Repository{FullName: "up/\x1b[2Jstream"},
	}
	safe := sanitizeRepo(r)
	var tty, raw bytes.Buffer
	printRepoTTY(&tty, safe)
	printRepoRaw(&raw, safe)
	for name, out := range map[string]string{"tty": tty.String(), "raw": raw.String()} {
		if strings.Contains(out, "\x1b") {
			t.Errorf("%s output contains ESC: %q", name, out)
		}
	}
	if !strings.Contains(raw.String(), "description:\tline one line two ^[]52;c;cHduZWQ=^G\n") {
		t.Errorf("raw description not on one safe line:\n%s", raw.String())
	}
	if r.Description == safe.Description || r.Parent.FullName == safe.Parent.FullName {
		t.Error("sanitizeRepo must not modify the original repository")
	}
}

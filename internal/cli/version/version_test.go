package version

import (
	"testing"

	"github.com/maymeow/fjo/internal/build"
)

func TestFormat(t *testing.T) {
	info := build.Info{Version: "0.1.0", Commit: "abc1234", Go: "go1.x", Platform: "linux/amd64"}
	want := "fjo version 0.1.0\ncommit: abc1234\ngo: go1.x\nplatform: linux/amd64\n"
	if got := Format(info); got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}

	info.Date = "2026-11-13"
	want = "fjo version 0.1.0\ncommit: abc1234\nbuilt: 2026-11-13\ngo: go1.x\nplatform: linux/amd64\n"
	if got := Format(info); got != want {
		t.Errorf("Format() with date = %q, want %q", got, want)
	}
}

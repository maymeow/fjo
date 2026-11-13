package build

import (
	"runtime"
	"testing"
)

func TestGetUsesLdflags(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })

	Version, Commit, Date = "1.2.3", "0123456789abcdef0123", "2026-11-13"
	info := Get()
	if info.Version != "1.2.3" || info.Date != "2026-11-13" {
		t.Errorf("ldflags values not used: %+v", info)
	}
	if info.Commit != "0123456789ab" {
		t.Errorf("commit should be shortened to 12 chars: %q", info.Commit)
	}
	if info.Go != runtime.Version() || info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Errorf("runtime info wrong: %+v", info)
	}
}

func TestGetDefaults(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })

	Version, Commit, Date = "", "", ""
	info := Get()
	if info.Version == "" || info.Commit == "" {
		t.Errorf("version and commit must never be empty: %+v", info)
	}
}

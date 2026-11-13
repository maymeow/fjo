// Package build holds version metadata injected at link time.
//
// Release builds set the variables with:
//
//	-ldflags "-X github.com/maymeow/fjo/internal/build.Version=0.1.0
//	          -X github.com/maymeow/fjo/internal/build.Commit=abc1234
//	          -X github.com/maymeow/fjo/internal/build.Date=2026-11-13"
package build

import (
	"runtime"
	"runtime/debug"
)

// Set via -ldflags. Empty values are filled from Go build info when possible.
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// Info describes the running binary.
type Info struct {
	Version  string
	Commit   string
	Date     string
	Go       string
	Platform string
}

// Get returns build information, falling back to module and VCS data embedded
// by the Go toolchain for development builds.
func Get() Info {
	info := Info{
		Version:  Version,
		Commit:   Commit,
		Date:     Date,
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			}
		}
	}

	if info.Version == "" {
		info.Version = "dev"
	}
	if len(info.Commit) > 12 {
		info.Commit = info.Commit[:12]
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	return info
}

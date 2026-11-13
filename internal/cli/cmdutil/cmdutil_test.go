package cmdutil

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestAddHostnameFlag(t *testing.T) {
	var short, long string
	shortCmd := &cobra.Command{Use: "short"}
	AddHostnameFlag(shortCmd, &short, true)
	longCmd := &cobra.Command{Use: "long"}
	AddHostnameFlag(longCmd, &long, false)

	if f := shortCmd.Flags().ShorthandLookup("h"); f == nil || f.Name != "hostname" {
		t.Error("-h should be a shorthand for --hostname")
	}
	if f := shortCmd.Flags().Lookup("help"); f == nil {
		t.Error("--help should be defined to avoid the -h clash")
	}
	if f := longCmd.Flags().ShorthandLookup("h"); f != nil {
		t.Errorf("-h should not be defined on %s", longCmd.Name())
	}
	host := shortCmd.Flags().Lookup("host")
	if host == nil || !host.Hidden {
		t.Error("--host should exist as a hidden alias")
	}

	if err := shortCmd.Flags().Parse([]string{"-h", "git.example.com"}); err != nil {
		t.Fatalf("parse -h: %v", err)
	}
	if short != "git.example.com" {
		t.Errorf("short value = %q", short)
	}
	if err := longCmd.Flags().Parse([]string{"--host", "codeberg.org"}); err != nil {
		t.Fatalf("parse --host: %v", err)
	}
	if long != "codeberg.org" {
		t.Errorf("long value = %q", long)
	}
}

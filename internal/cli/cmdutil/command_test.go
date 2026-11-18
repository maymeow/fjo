package cmdutil

import "testing"

func TestOneLine(t *testing.T) {
	for in, want := range map[string]string{
		"  two\twords\n":       "two words",
		"title\x1b]52;c;x\x07": "title^[]52;c;x^G",
		"":                     "",
	} {
		if got := OneLine(in); got != want {
			t.Errorf("OneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

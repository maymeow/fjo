package pr

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintDiffKeepsRunesAcrossBufferBoundaries(t *testing.T) {
	input := "+" + strings.Repeat("x", 4094) + "ž\x1b[2J\n"
	var out bytes.Buffer
	if err := printDiff(&out, strings.NewReader(input), false); err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(input, "\x1b", "^[", 1); out.String() != want {
		t.Fatal("diff corrupted the UTF-8 rune or left a terminal escape sequence")
	}
}

package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/iostreams"
)

func newTerminal(input string) (*Terminal, *bytes.Buffer) {
	ios, in, _, errOut := iostreams.Test()
	in.WriteString(input)
	return New(ios), errOut
}

func TestInput(t *testing.T) {
	term, errOut := newTerminal("hello\r\n")
	got, err := term.Input("Name:")
	if err != nil {
		t.Fatalf("Input: %v", err)
	}
	if got != "hello" {
		t.Errorf("Input = %q, want %q", got, "hello")
	}
	if !strings.Contains(errOut.String(), "? Name:") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestInputEOFIsCancelled(t *testing.T) {
	term, _ := newTerminal("")
	_, err := term.Input("Name:")
	if fjoerr.KindOf(err) != fjoerr.KindCancelled {
		t.Fatalf("err = %v, want cancelled", err)
	}
}

func TestInputAllowsLastLineWithoutNewline(t *testing.T) {
	term, _ := newTerminal("value")
	got, err := term.Input("Name:")
	if err != nil || got != "value" {
		t.Fatalf("Input = %q, %v", got, err)
	}
}

func TestPasswordNonTerminal(t *testing.T) {
	term, errOut := newTerminal("s3cret\n")
	got, err := term.Password("Token:")
	if err != nil {
		t.Fatalf("Password: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("Password = %q", got)
	}
	if !strings.Contains(errOut.String(), "? Token:") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestSelect(t *testing.T) {
	term, errOut := newTerminal("2\n")
	i, err := term.Select("Pick one", []string{"a", "b"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if i != 1 {
		t.Errorf("Select = %d, want 1", i)
	}
	if !strings.Contains(errOut.String(), "1) a") || !strings.Contains(errOut.String(), "2) b") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestSelectRetriesInvalidInput(t *testing.T) {
	term, _ := newTerminal("0\n3\nx\n2\n")
	i, err := term.Select("Pick one", []string{"a", "b"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if i != 1 {
		t.Errorf("Select = %d, want 1", i)
	}
}

func TestSelectRejectsEmptyOptions(t *testing.T) {
	term, _ := newTerminal("")
	if _, err := term.Select("Pick one", nil); err == nil {
		t.Fatal("Select with no options should fail")
	}
}

func TestSelectEOFIsCancelled(t *testing.T) {
	term, _ := newTerminal("")
	_, err := term.Select("Pick one", []string{"a"})
	if fjoerr.KindOf(err) != fjoerr.KindCancelled {
		t.Fatalf("err = %v, want cancelled", err)
	}
}

package fjoerr

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExitCodeOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ExitCode
	}{
		{"nil", nil, ExitOK},
		{"plain", errors.New("boom"), ExitError},
		{"usage", Usage("bad flag"), ExitUsage},
		{"auth", New(KindAuth, "login required"), ExitAuth},
		{"cancel", New(KindCancelled, "cancelled"), ExitCancel},
		{"not found wrapped", fmt.Errorf("ctx: %w", New(KindNotFound, "nope")), ExitNotFound},
		{"forbidden", New(KindForbidden, "x"), ExitForbidden},
		{"conflict", New(KindConflict, "x"), ExitConflict},
		{"unavailable", New(KindUnavailable, "x"), ExitUnavailable},
		{"unsupported", New(KindUnsupported, "x"), ExitUnsupported},
		{"pending", New(KindPending, "x"), ExitPending},
		{"config", New(KindConfig, "x"), ExitError},
		{"deadline", fmt.Errorf("req: %w", context.DeadlineExceeded), ExitTimeout},
		{"canceled", context.Canceled, ExitInterrupted},
		{"unknown kind", &Error{Kind: "made_up"}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCodeOf(tt.err); got != tt.want {
				t.Errorf("ExitCodeOf() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGhCompatibleCodes(t *testing.T) {
	// These values are a public contract shared with gh (IMPLEMENTATION §1.4).
	fixed := map[ExitCode]int{ExitOK: 0, ExitError: 1, ExitCancel: 2, ExitAuth: 4, ExitPending: 8}
	for code, want := range fixed {
		if int(code) != want {
			t.Errorf("exit code %d changed, want %d", code, want)
		}
	}
	for _, code := range []ExitCode{ExitUsage, ExitForbidden, ExitNotFound, ExitConflict, ExitUnavailable, ExitUnsupported, ExitTimeout} {
		if code <= ExitPending {
			t.Errorf("fjo-specific exit code %d must be above gh range", code)
		}
	}
}

func TestErrorMessageAndUnwrap(t *testing.T) {
	inner := errors.New("inner")
	e := Wrap(KindUnavailable, inner, "connect to %s", "example.com").WithHint("check network")
	if e.Error() != "connect to example.com: inner" {
		t.Errorf("Error() = %q", e.Error())
	}
	if !errors.Is(e, inner) {
		t.Error("Wrap must keep inner error in chain")
	}
	if HintOf(fmt.Errorf("outer: %w", e)) != "check network" {
		t.Error("HintOf must find hint through wrapping")
	}
	if (&Error{Kind: KindAuth}).Error() != "auth_required" {
		t.Error("empty error should fall back to kind")
	}
}

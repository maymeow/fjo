// Package fjoerr defines the typed error model shared by all fjo layers and
// its mapping to process exit codes.
//
// Exit codes follow GitHub CLI where gh defines a meaning (0, 1, 2, 4, 8) and
// add fjo-specific codes only above gh's range (IMPLEMENTATION §1.4, §10.4).
package fjoerr

import (
	"context"
	"errors"
	"fmt"
)

// ExitCode is a process exit status.
type ExitCode int

// Exit codes. Values 0–8 match gh; 10+ are fjo extensions.
const (
	ExitOK          ExitCode = 0
	ExitError       ExitCode = 1 // general failure (gh: 1)
	ExitCancel      ExitCode = 2 // user cancelled a prompt (gh: 2)
	ExitAuth        ExitCode = 4 // authentication required or failed (gh: 4)
	ExitPending     ExitCode = 8 // checks still pending (gh: 8); reserved
	ExitUsage       ExitCode = 10
	ExitForbidden   ExitCode = 11
	ExitNotFound    ExitCode = 12
	ExitConflict    ExitCode = 13
	ExitUnavailable ExitCode = 14
	ExitUnsupported ExitCode = 15
	ExitTimeout     ExitCode = 16
	ExitInterrupted ExitCode = 130
)

// Kind classifies an error independently of its message.
type Kind string

// Error kinds. The string values are part of the machine-readable error
// contract (§10.3) and must stay stable.
const (
	KindGeneral     Kind = "error"
	KindUsage       Kind = "invalid_usage"
	KindCancelled   Kind = "cancelled"
	KindAuth        Kind = "auth_required"
	KindForbidden   Kind = "forbidden"
	KindNotFound    Kind = "not_found"
	KindConflict    Kind = "conflict"
	KindUnavailable Kind = "unavailable"
	KindUnsupported Kind = "unsupported"
	KindTimeout     Kind = "timeout"
	KindInterrupted Kind = "interrupted"
	KindPending     Kind = "pending"
	KindConfig      Kind = "invalid_config"
)

var exitCodes = map[Kind]ExitCode{
	KindGeneral:     ExitError,
	KindUsage:       ExitUsage,
	KindCancelled:   ExitCancel,
	KindAuth:        ExitAuth,
	KindForbidden:   ExitForbidden,
	KindNotFound:    ExitNotFound,
	KindConflict:    ExitConflict,
	KindUnavailable: ExitUnavailable,
	KindUnsupported: ExitUnsupported,
	KindTimeout:     ExitTimeout,
	KindInterrupted: ExitInterrupted,
	KindPending:     ExitPending,
	KindConfig:      ExitError,
}

// Error is a classified fjo error. Message must never contain secrets.
type Error struct {
	Kind    Kind
	Message string
	// Hint is an optional actionable suggestion shown below the message.
	Hint string
	Err  error
	// Silent errors set the exit code but print nothing: the command has
	// already reported the problem (e.g. `auth status`).
	Silent bool
}

func (e *Error) Error() string {
	switch {
	case e.Message != "" && e.Err != nil:
		return e.Message + ": " + e.Err.Error()
	case e.Message != "":
		return e.Message
	case e.Err != nil:
		return e.Err.Error()
	default:
		return string(e.Kind)
	}
}

func (e *Error) Unwrap() error { return e.Err }

// New returns a classified error with a formatted message.
func New(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Wrap classifies err, keeping it in the chain.
func Wrap(kind Kind, err error, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...), Err: err}
}

// Usage returns an invalid-usage error.
func Usage(format string, args ...any) *Error { return New(KindUsage, format, args...) }

// Silent returns an error that only carries an exit code.
func Silent(kind Kind) *Error { return &Error{Kind: kind, Silent: true} }

// IsSilent reports whether err should not be printed.
func IsSilent(err error) bool {
	var fe *Error
	return errors.As(err, &fe) && fe.Silent
}

// WithHint returns e with an actionable hint attached.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// KindOf returns the kind of the first *Error in err's chain. Context
// cancellation and deadlines are classified even when not wrapped.
func KindOf(err error) Kind {
	if err == nil {
		return ""
	}
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	case errors.Is(err, context.Canceled):
		return KindInterrupted
	}
	return KindGeneral
}

// ExitCodeOf maps err to a process exit code.
func ExitCodeOf(err error) ExitCode {
	if err == nil {
		return ExitOK
	}
	if code, ok := exitCodes[KindOf(err)]; ok {
		return code
	}
	return ExitError
}

// HintOf returns the hint of the first *Error in err's chain, if any.
func HintOf(err error) string {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Hint
	}
	return ""
}

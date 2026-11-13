package logging

import (
	"context"
	"io"
	"log/slog"
)

// Level selects how much diagnostic output is produced on stderr.
type Level int

// Levels: Quiet (default) emits nothing, Verbose emits user-level progress,
// Debug adds sanitized protocol details.
const (
	Quiet Level = iota
	Verbose
	Debug
)

// New returns a logger writing redacted text records to w.
// With Quiet the logger discards everything.
func New(w io.Writer, level Level, r *Redactor) *slog.Logger {
	if level == Quiet {
		return slog.New(slog.DiscardHandler)
	}
	slogLevel := slog.LevelInfo
	if level == Debug {
		slogLevel = slog.LevelDebug
	}
	inner := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slogLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{} // stable, diff-friendly output
			}
			return a
		},
	})
	return slog.New(&redactingHandler{inner: inner, r: r})
}

// redactingHandler redacts the message and all string-like attribute values
// before they reach the underlying handler.
type redactingHandler struct {
	inner slog.Handler
	r     *Redactor
}

func (h *redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.String(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.redactAttr(a)
	}
	return &redactingHandler{inner: h.inner.WithAttrs(red), r: h.r}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name), r: h.r}
}

func (h *redactingHandler) redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindGroup:
		group := v.Group()
		red := make([]any, len(group))
		for i, ga := range group {
			red[i] = h.redactAttr(ga)
		}
		return slog.Group(a.Key, red...)
	case slog.KindString:
		return slog.String(a.Key, h.r.String(v.String()))
	case slog.KindAny:
		// Stringify arbitrary values (errors, URLs, headers) so they pass
		// through the redactor instead of being formatted later.
		return slog.String(a.Key, h.r.String(v.String()))
	default:
		return slog.Attr{Key: a.Key, Value: v}
	}
}

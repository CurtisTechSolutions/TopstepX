// Package logx provides the SDK's default logger, which discards every
// record. It avoids slog.DiscardHandler, which needs Go 1.24.
package logx

import (
	"context"
	"log/slog"
)

// discardHandler is a slog.Handler that drops every record.
type discardHandler struct{}

// Enabled reports false for every level.
func (discardHandler) Enabled(context.Context, slog.Level) bool { return false }

// Handle drops the record.
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }

// WithAttrs returns the handler itself.
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler { return d }

// WithGroup returns the handler itself.
func (d discardHandler) WithGroup(string) slog.Handler { return d }

// Discard returns a logger that drops every record.
func Discard() *slog.Logger { return slog.New(discardHandler{}) }

// OrDiscard returns l, or a discarding logger when l is nil.
func OrDiscard(l *slog.Logger) *slog.Logger {
	if l == nil {
		return Discard()
	}
	return l
}

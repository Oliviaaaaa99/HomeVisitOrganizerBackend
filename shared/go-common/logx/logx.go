// Package logx wraps log/slog with a consistent app-wide configuration.
package logx

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey struct{}

// New returns a JSON slog logger writing to stderr.
func New(service string, level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(h).With("service", service)
}

// Into stores the logger on the context.
func Into(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From retrieves the logger from context, falling back to slog.Default.
func From(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

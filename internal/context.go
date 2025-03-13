/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package internal

import (
	"context"
	"log/slog"
)

// contextKey is the type used to store the tool in the context.
type contextKey int

const (
	contextToolKey contextKey = iota
	contextLoggerKey
)

// ToolFromContext returns the tool from the context. It panics if the given context doesn't contain
// the tool.
func ToolFromContext(ctx context.Context) *Tool {
	tool := ctx.Value(contextToolKey).(*Tool)
	if tool == nil {
		panic("failed to get tool from context")
	}
	return tool
}

// ToolIntoContext creates a new context that contains the given tool.
func ToolIntoContext(ctx context.Context, tool *Tool) context.Context {
	return context.WithValue(ctx, contextToolKey, tool)
}

// LoggerFromContext returns the logger from the context. It panics if the given context doesn't
// contain a logger.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	logger := ctx.Value(contextLoggerKey).(*slog.Logger)
	if logger == nil {
		panic("failed to get logger from context")
	}
	return logger
}

// LoggerIntoContext creates a new context that contains the given logger.
func LoggerIntoContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextLoggerKey, logger)
}

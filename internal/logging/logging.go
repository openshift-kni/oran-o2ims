/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package logging

import (
	"context"
	"log/slog"
)

//
// This module includes utilities to define a slog handler that includes attributes
// that have been added to the context in order to carry info through an execution
// flow without needing to explicitly include it in all logs.
//

type loggingContextKey string

const (
	slogFields loggingContextKey = "slog_fields"
)

type LoggingContextHandler struct {
	handler slog.Handler
	level   slog.Level
}

// Handle adds attributes from the context to the log record
func (h LoggingContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if attrs, ok := ctx.Value(slogFields).([]slog.Attr); ok {
		for _, v := range attrs {
			record.AddAttrs(v)
		}
	}

	return h.handler.Handle(ctx, record) // nolint: wrapcheck
}

func (h LoggingContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h LoggingContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return LoggingContextHandler{handler: h.handler.WithAttrs(attrs), level: h.level}
}

func (h LoggingContextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return LoggingContextHandler{handler: h.handler.WithGroup(name), level: h.level}
}

// NewContextHandler wraps a base slog.Handler with context attribute extraction.
func NewContextHandler(base slog.Handler, level slog.Level) *LoggingContextHandler {
	return &LoggingContextHandler{handler: base, level: level}
}

// AppendCtx adds an slog attribute to the provided context so that it will be
// included in any Record created with such context. If an attribute with the
// same key already exists, it is replaced rather than duplicated.
func AppendCtx(ctx context.Context, attr slog.Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if v, ok := ctx.Value(slogFields).([]slog.Attr); ok {
		newV := make([]slog.Attr, len(v))
		copy(newV, v)
		for i, existing := range newV {
			if existing.Key == attr.Key {
				newV[i] = attr
				return context.WithValue(ctx, slogFields, newV)
			}
		}
		newV = append(newV, attr)
		return context.WithValue(ctx, slogFields, newV)
	}

	return context.WithValue(ctx, slogFields, []slog.Attr{attr})
}

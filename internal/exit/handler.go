/*
Copyright 2024 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package exit

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
)

// HandlerBuilder contains the data and logic needed to build an exit handler.
type HandlerBuilder struct {
	logger  *slog.Logger
	signals []os.Signal
}

// Handler knows how to wait for exit signals and how to execute exit actions before exiting.
type Handler struct {
	logger  *slog.Logger
	signals []os.Signal
	actions []func(ctx context.Context) error
}

// NewHandler creates a builder that can then be used to configure and create an exit handler.
func NewHandler() *HandlerBuilder {
	return &HandlerBuilder{
		signals: []os.Signal{syscall.SIGINT, syscall.SIGTERM},
	}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *HandlerBuilder) SetLogger(logger *slog.Logger) *HandlerBuilder {
	b.logger = logger
	return b
}

// AddSignal adds a shutdown signal. Signals SIGINT and SIGTERM are included by default.
func (b *HandlerBuilder) AddSignal(value os.Signal) *HandlerBuilder {
	b.signals = append(b.signals, value)
	return b
}

// AddSignals adds a list of exit signal. Signals SIGINT and SIGTERM are included by default.
func (b *HandlerBuilder) AddSignals(values ...os.Signal) *HandlerBuilder {
	b.signals = append(b.signals, values...)
	return b
}

// SetSignal sets the exit signal, discarding any signals that have been previosly configured,
// including the defaults.
func (b *HandlerBuilder) SetSignal(value os.Signal) *HandlerBuilder {
	b.signals = append(b.signals, value)
	return b
}

// SetSignals sets a list of exit signal, discarding any signals that have been previosly
// configured, including the defaults.
func (b *HandlerBuilder) SetSignals(values ...os.Signal) *HandlerBuilder {
	b.signals = append(b.signals, values...)
	return b
}

// Build uses the data stored in the builder to create and configure a new exit handler.
func (b *HandlerBuilder) Build() (result *Handler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if len(b.signals) == 0 {
		err = errors.New("at least one signal is required")
		return
	}

	// Create and populate the object:
	result = &Handler{
		logger:  b.logger,
		signals: slices.Clone(b.signals),
	}
	return
}

// AddAction adds an action that will be executed prior to exiting.
func (h *Handler) AddAction(value func(ctx context.Context) error) {
	h.actions = append(h.actions, value)
}

// AddServer adds an HTTP server that should be shutdown prior to exiting.
func (h *Handler) AddServer(value *http.Server) {
	h.actions = append(
		h.actions,
		func(ctx context.Context) error {
			return h.shutdownServer(ctx, value)
		},
	)
}

// Wait waits for an exit signal. When it is received it will perform all the registered exit
// actions and then it will exit the process.
func (h *Handler) Wait(ctx context.Context) error {
	// Configure the process to receive the signals:
	c := make(chan os.Signal, 2)
	signal.Notify(c, h.signals...)

	// Wait for the first signal and then run the exit actions:
	names := make([]string, len(h.signals))
	for i, s := range h.signals {
		names[i] = s.String()
		s.Signal()
	}
	h.logger.InfoContext(
		ctx,
		"Waiting for exit signals",
		slog.Any("signals", names),
	)
	s := <-c
	go func() {
		h.logger.InfoContext(
			ctx,
			"Received exit signal",
			slog.String("signal", s.String()),
		)
		for _, action := range h.actions {
			err := action(ctx)
			if err != nil {
				h.logger.ErrorContext(
					ctx,
					"Failed to run exit action",
					slog.String("error", err.Error()),
				)
			}
		}
		os.Exit(0)
	}()

	// If we receive a second signal then we stop inmediately, without waiting for the exit
	// actions to complete.
	s = <-c
	h.logger.InfoContext(
		ctx,
		"Received signal while waiting for actions to complete",
		slog.String("signal", s.String()),
	)
	os.Exit(1)

	return nil
}

func (h *Handler) shutdownServer(ctx context.Context, server *http.Server) error {
	h.logger.InfoContext(
		ctx,
		"Shutting down server",
		slog.String("address", server.Addr),
	)
	err := server.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this file except in compliance with the License. You may obtain a copy of the
License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jhernand/o2ims/internal/filter"
)

var _ http.Handler = (*Server)(nil)

// ServerBuilder contains the data and logic needed to create servers. Don't create instances
// of this type directly, use the NewServer function instead.
type ServerBuilder struct {
	logger  *slog.Logger
	handler Handler
}

// Server knows how to process HTTP requests that include search filters. Don't create instances
// of this type directly, use the NewServer function instead.
type Server struct {
	logger  *slog.Logger
	parser  *filter.Parser
	handler Handler
}

// NewServer creates a builder that can then be used to create and configure servers.
func NewServer() *ServerBuilder {
	return &ServerBuilder{}
}

// SetLogger sets the logger that the server will use to write to the log. This is mandatory.
func (b *ServerBuilder) SetLogger(logger *slog.Logger) *ServerBuilder {
	b.logger = logger
	return b
}

// SetHandler sets the object that will handle the requests. This is mandatory.
func (b *ServerBuilder) SetHandler(value Handler) *ServerBuilder {
	b.handler = value
	return b
}

// Build uses the data stored in the builder to create and configure a new server.
func (b *ServerBuilder) Build() (result *Server, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.handler == nil {
		err = errors.New("handler is mandatory")
		return
	}

	// Create the filter expression parser:
	parser, err := filter.NewParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create filter expression parser: %w", err)
		return
	}

	// Create and populate the object:
	result = &Server{
		logger:  b.logger,
		parser:  parser,
		handler: b.handler,
	}
	return
}

// Serve is the implementation of the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.logger.Info(
		"Received request",
		"from", r.RemoteAddr,
		"url", r.URL,
	)
	query := r.URL.Query()

	// Create the request:
	request := &Request{
		HTTP: r,
	}

	// Check if there is a filter expression, and parse it:
	text := query.Get("filter")
	if text != "" {
		var err error
		request.Filter, err = s.parser.Parse(text)
		if err != nil {
			s.logger.Error(
				"Failed to parse filter expression",
				slog.String("filter", text),
				slog.String("error", err.Error()),
			)
			s.sendError(w, "Failed to parse filter expression '%s': %v", text, err)
			return
		}
		s.logger.Info(
			"Parsed filter expression",
			slog.String("text", text),
			slog.String("result", request.Filter.String()),
		)
	}

	// Call the handler:
	s.handler.Serve(w, request)
}

func (s *Server) sendError(w http.ResponseWriter, msg string, args ...any) {
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, msg, args...)
}

/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package openapi

import (
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"gopkg.in/yaml.v3"
)

//go:embed spec.yaml
var dataFS embed.FS

// HandlerBuilder contains the data and logic needed to create a new handler for the OpenAPI
// metadata. Don't create instances of this type directly, use the NewHandler function instead.
type HandlerBuilder struct {
	logger *slog.Logger
}

// Handler knows how to respond to requests for the OpenAPI metadata. Don't create instances of
// this type directly, use the NewHandler function instead.
type Handler struct {
	logger *slog.Logger
	spec   []byte
}

// NewHandler creates a builder that can then be used to configure and create a handler for the
// OpenAPI metadata.
func NewHandler() *HandlerBuilder {
	return &HandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *HandlerBuilder) SetLogger(value *slog.Logger) *HandlerBuilder {
	b.logger = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *HandlerBuilder) Build() (result *Handler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Load the specification:
	spec, err := b.loadSpec()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &Handler{
		logger: b.logger,
		spec:   spec,
	}
	return
}

func (b *HandlerBuilder) loadSpec() (result []byte, err error) {
	// Read the main file:
	data, err := dataFS.ReadFile("spec.yaml")
	if err != nil {
		return
	}
	var spec any
	err = yaml.Unmarshal(data, &spec)
	if err != nil {
		return
	}
	result, err = json.Marshal(spec)
	return
}

// ServeHTTP is the implementation of the object HTTP handler interface.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get the context:
	ctx := r.Context()

	// Send the response:
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(h.spec)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to send data",
			slog.String("error", err.Error()),
		)
	}
}

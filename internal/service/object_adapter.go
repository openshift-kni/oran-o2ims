/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/jhernand/o2ims/internal/data"
	jsoniter "github.com/json-iterator/go"
)

type ObjectAdapterBuilder struct {
	logger  *slog.Logger
	handler ObjectHandler
	id      string
}

type ObjectAdapter struct {
	logger  *slog.Logger
	handler ObjectHandler
	id      string
	api     jsoniter.API
}

func NewObjectAdapter() *ObjectAdapterBuilder {
	return &ObjectAdapterBuilder{}
}

// SetLogger sets the logger that the server will use to write to the log. This is mandatory.
func (b *ObjectAdapterBuilder) SetLogger(logger *slog.Logger) *ObjectAdapterBuilder {
	b.logger = logger
	return b
}

// SetHandler sets the object that will handle the requests. This is mandatory.
func (b *ObjectAdapterBuilder) SetHandler(value ObjectHandler) *ObjectAdapterBuilder {
	b.handler = value
	return b
}

// SetID sets the name of the path variable that contains the identifier of the object. This is
// mandatory.
func (b *ObjectAdapterBuilder) SetID(value string) *ObjectAdapterBuilder {
	b.id = value
	return b
}

// Build uses the data stored in the builder to create and configure a new adapter.
func (b *ObjectAdapterBuilder) Build() (result *ObjectAdapter, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.handler == nil {
		err = errors.New("handler is mandatory")
		return
	}
	if b.id == "" {
		err = errors.New("name of path variable containing identifier is mandatory")
		return
	}

	// Prepare the JSON iterator API:
	cfg := jsoniter.Config{
		IndentionStep: 2,
	}
	api := cfg.Froze()

	// Create and populate the object:
	result = &ObjectAdapter{
		logger:  b.logger,
		handler: b.handler,
		id:      b.id,
		api:     api,
	}
	return
}

// Serve is the implementation of the http.Handler interface.
func (a *ObjectAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.logger.Info(
		"Received request",
		"from", r.RemoteAddr,
		"url", r.URL.String(),
	)

	// Get the context:
	ctx := r.Context()

	// Get the identifier:
	id, ok := mux.Vars(r)[a.id]
	if !ok {
		a.logger.Error(
			"Failed to find path variable",
			"var", a.id,
		)
		SendError(
			w,
			http.StatusInternalServerError,
			"Failed to find path variable",
		)
		return
	}

	// Create the request:
	request := &ObjectRequest{
		ID: id,
	}

	// Call the handler:
	response, err := a.handler.Get(r.Context(), request)
	if err != nil {
		a.logger.Error(
			"Failed to get items",
			"error", err,
		)
		SendError(
			w,
			http.StatusInternalServerError,
			"Failed to get items",
		)
		return
	}

	// Send the result:
	a.sendObject(ctx, w, response.Object)
}

func (a *ObjectAdapter) sendObject(ctx context.Context, w http.ResponseWriter,
	object data.Object) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writer := jsoniter.NewStream(a.api, w, 0)
	writer.WriteVal(object)
}

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
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

type ObjectAdapterBuilder struct {
	logger     *slog.Logger
	handler    ObjectHandler
	idVariable string
}

type ObjectAdapter struct {
	logger             *slog.Logger
	idVariable         string
	projectorParser    *search.ProjectorParser
	projectorEvaluator *search.ProjectorEvaluator
	jsonAPI            jsoniter.API
	handler            ObjectHandler
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

// SetIDVariable sets the name of the path variable that contains the identifier of the object. This is
// optional. If not specified then no identifier will be passed to the handler.
func (b *ObjectAdapterBuilder) SetIDVariable(value string) *ObjectAdapterBuilder {
	b.idVariable = value
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

	// Create the projector parser and evaluator:
	projectorParser, err := search.NewProjectorParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create field selector parser: %w", err)
		return
	}
	pathEvaluator, err := search.NewPathEvaluator().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create projector path evaluator: %w", err)
		return
	}
	projectorEvaluator, err := search.NewProjectorEvaluator().
		SetLogger(b.logger).
		SetPathEvaluator(pathEvaluator.Evaluate).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create projector evaluator: %w", err)
		return
	}

	// Prepare the JSON iterator API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create and populate the object:
	result = &ObjectAdapter{
		logger:             b.logger,
		handler:            b.handler,
		idVariable:         b.idVariable,
		projectorParser:    projectorParser,
		projectorEvaluator: projectorEvaluator,
		jsonAPI:            jsonAPI,
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

	// Get the query parameters:
	query := r.URL.Query()

	// Create the request:
	request := &ObjectRequest{
		ID: mux.Vars(r)[a.idVariable],
	}

	// Check if there is a projector, and parse it:
	values, ok := query["fields"]
	if ok {
		for _, value := range values {
			projector, err := a.projectorParser.Parse(value)
			if err != nil {
				a.logger.Error(
					"Failed to parse field selector",
					slog.String("selector", value),
					slog.String("error", err.Error()),
				)
				SendError(
					w,
					http.StatusBadRequest,
					"Failed to parse field selector '%s': %v",
					value, err,
				)
				return
			}
			a.logger.Debug(
				"Parsed field selector",
				slog.String("source", value),
				slog.Any("parsed", projector),
			)
			request.Projector = append(request.Projector, projector...)
		}
	}

	// Call the handler:
	response, err := a.handler.Get(ctx, request)
	if err != nil {
		a.logger.Error(
			"Failed to get items",
			"error", err,
		)
		SendError(
			w,
			http.StatusInternalServerError,
			"Internal error",
		)
		return
	}

	// If there is a projector apply it:
	object := response.Object
	if request.Projector != nil {
		object, err = a.projectorEvaluator.Evaluate(ctx, request.Projector, response.Object)
		if err != nil {
			a.logger.Error(
				"Failed to evaluate projector",
				slog.String("error", err.Error()),
			)
			SendError(
				w,
				http.StatusInternalServerError,
				"Internal error",
			)
			return
		}
	}

	// Send the result:
	a.sendObject(ctx, w, object)
}

func (a *ObjectAdapter) sendObject(ctx context.Context, w http.ResponseWriter,
	object data.Object) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writer := jsoniter.NewStream(a.jsonAPI, w, 0)
	writer.WriteVal(object)
	if writer.Error != nil {
		a.logger.Error(
			"Failed to send object",
			"error", writer.Error.Error(),
		)
	}
	writer.Flush()
	if writer.Error != nil {
		a.logger.Error(
			"Failed to flush stream",
			"error", writer.Error.Error(),
		)
	}
}

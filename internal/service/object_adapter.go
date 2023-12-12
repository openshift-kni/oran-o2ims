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
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/search"
	"github.com/openshift-kni/oran-o2ims/internal/streaming"
)

type ObjectAdapterBuilder struct {
	logger           *slog.Logger
	handler          ObjectHandler
	defaultInclude   []string
	defaultExclude   []string
	idVariable       string
	parentIdVariable string
}

type ObjectAdapter struct {
	logger             *slog.Logger
	idVariable         string
	parentIdVariable   string
	pathsParser        *search.PathsParser
	projectorEvaluator *search.ProjectorEvaluator
	defaultInclude     []search.Path
	defaultExclude     []search.Path
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

// SetDefaultInclude sets the collection of fields that will be included by default from the
// response. Each field is specified using the same syntax accepted by projector parsers.
func (b *ObjectAdapterBuilder) SetDefaultInclude(values ...string) *ObjectAdapterBuilder {
	b.defaultInclude = append(b.defaultExclude, values...)
	return b
}

// SetDefaultExclude sets the collection of fields that will be excluded by default from the
// response. Each field is specified using the same syntax accepted by projector parsers.
func (b *ObjectAdapterBuilder) SetDefaultExclude(values ...string) *ObjectAdapterBuilder {
	b.defaultExclude = append(b.defaultExclude, values...)
	return b
}

// SetIDVariable sets the name of the path variable that contains the identifier of the object. This is
// optional. If not specified then no identifier will be passed to the handler.
func (b *ObjectAdapterBuilder) SetIDVariable(value string) *ObjectAdapterBuilder {
	b.idVariable = value
	return b
}

// SetCollectionIDVariable sets the name of the path variable that contains the identifier of the parent collection.
// This is optional. If not specified then no identifier will be passed to the handler.
func (b *ObjectAdapterBuilder) SetParentIDVariable(value string) *ObjectAdapterBuilder {
	b.parentIdVariable = value
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

	// Create the paths parser:
	pathsParser, err := search.NewPathsParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}

	// Create the projector evaluator:
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

	// Parse the default paths:
	defaultInclude, err := pathsParser.Parse(b.defaultInclude...)
	if err != nil {
		return
	}
	defaultExclude, err := pathsParser.Parse(b.defaultExclude...)
	if err != nil {
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
		parentIdVariable:   b.parentIdVariable,
		pathsParser:        pathsParser,
		projectorEvaluator: projectorEvaluator,
		defaultInclude:     defaultInclude,
		defaultExclude:     defaultExclude,
		jsonAPI:            jsonAPI,
	}
	return
}

// Serve is the implementation of the http.Handler interface.
func (a *ObjectAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error

	// Get the context:
	ctx := r.Context()

	// Get the query parameters:
	query := r.URL.Query()

	// Create the request:
	request := &GetRequest{
		ID:       mux.Vars(r)[a.idVariable],
		ParentID: mux.Vars(r)[a.parentIdVariable],
		Projector: &search.Projector{
			Include: a.defaultInclude,
			Exclude: a.defaultExclude,
		},
	}

	// Check if there is a projector, and parse it:
	includeFields, ok := query["fields"]
	if ok {
		request.Projector.Include, err = a.pathsParser.Parse(includeFields...)
		if err != nil {
			a.logger.Error(
				"Failed to parse included fields",
				slog.Any("fields", includeFields),
				slog.String("error", err.Error()),
			)
			SendError(
				w,
				http.StatusBadRequest,
				"Failed to parse 'fields' parameter with values %s: %v",
				logging.All(includeFields), err,
			)
			return
		}
	}
	excludeFields, ok := query["exclude_fields"]
	if ok {
		request.Projector.Exclude, err = a.pathsParser.Parse(excludeFields...)
		if err != nil {
			a.logger.Error(
				"Failed to parse excluded fields",
				slog.Any("fields", excludeFields),
				slog.String("error", err.Error()),
			)
			SendError(
				w,
				http.StatusBadRequest,
				"Failed to parse 'exclude_fields' parameter with values %s: %v",
				logging.All(includeFields), err,
			)
		}
	}
	if request.Projector.Empty() {
		request.Projector = nil
	}

	// Call the handler:
	response, err := a.handler.Get(ctx, request)
	if err != nil {
		a.logger.Error(
			"Failed to get items",
			"error", err,
		)
		if errors.Is(err, streaming.ErrEnd) {
			SendError(
				w,
				http.StatusNotFound,
				"Not found",
			)
			return
		}
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

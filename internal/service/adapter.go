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
	"slices"

	"github.com/gorilla/mux"
	jsoniter "github.com/json-iterator/go"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/search"
	"github.com/openshift-kni/oran-o2ims/internal/streaming"
)

// AdapterBuilder contains the data and logic needed to create adapters. Don't create instances of
// this type directly, use the NewAdapter function instead.
type AdapterBuilder struct {
	logger        *slog.Logger
	pathVariables []string
	handler       any
	includeFields []string
	excludeFields []string
}

// Adapter knows how to translate an HTTP request into a request for a collection of objects. Don't
// create instances of this type directly, use the NewAdapter function instead.
type Adapter struct {
	logger             *slog.Logger
	pathVariables      []string
	collectionHandler  CollectionHandler
	objectHandler      ObjectHandler
	includeFields      []search.Path
	excludeFields      []search.Path
	pathsParser        *search.PathsParser
	selectorParser     *search.SelectorParser
	projectorEvaluator *search.ProjectorEvaluator
	jsonAPI            jsoniter.API
}

// NewAdapter creates a builder that can be used to configure and create an adatper.
func NewAdapter() *AdapterBuilder {
	return &AdapterBuilder{}
}

// SetLogger sets the logger that the adapter will use to write to the log. This is mandatory.
func (b *AdapterBuilder) SetLogger(logger *slog.Logger) *AdapterBuilder {
	b.logger = logger
	return b
}

// SetHandler sets the object that will handle the requests. The value must implement at least one
// of the handler interfaces. This is mandatory.
func (b *AdapterBuilder) SetHandler(value any) *AdapterBuilder {
	b.handler = value
	return b
}

// SetPathVariables sets the names of the path variables, in the same order that they appear in the
// path. For example, if the path is the following:
//
// /o2ims-infrastructureEnvironment/v1/resourcePools/{resourcePoolId}/resources/{resourceId}
//
// Then the values passed to this method should be `resourcePoolId` and `resourceId`.
//
// At least one path variable is required.
func (b *AdapterBuilder) SetPathVariables(values ...string) *AdapterBuilder {
	b.pathVariables = values
	return b
}

// SetIncludeFields set thes collection of fields that will be included by default from the
// response. Each field is specified using the same syntax accepted by paths parsers.
func (b *AdapterBuilder) SetIncludeFields(values ...string) *AdapterBuilder {
	b.includeFields = append(b.excludeFields, values...)
	return b
}

// SetExcludeFields sets the collection of fields that will be excluded by default from the
// response. Each field is specified using the same syntax accepted by paths parsers.
func (b *AdapterBuilder) SetExcludeFields(values ...string) *AdapterBuilder {
	b.excludeFields = append(b.excludeFields, values...)
	return b
}

// Build uses the data stored in the builder to create and configure a new adapter.
func (b *AdapterBuilder) Build() (result *Adapter, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.handler == nil {
		err = errors.New("handler is mandatory")
		return
	}

	// Check that the handler implements at least one of the handler interfaces:
	collectionHandler, _ := b.handler.(CollectionHandler)
	objectHandler, _ := b.handler.(ObjectHandler)
	if collectionHandler == nil && objectHandler == nil {
		err = errors.New("handler doesn't implement any of the handler interfaces")
		return
	}

	// If the handler implements the collection and object handler interfaces then we need to
	// have at least one path variable because we use it to decide if the request is for the
	// collection or for a specific object.
	if collectionHandler != nil && objectHandler != nil && len(b.pathVariables) == 0 {
		err = errors.New(
			"at least one path variable is required when both the collection and " +
				"object handlers are implemented",
		)
		return
	}

	// We store the variables in reverse order because most handlers will only need the last
	// one, and that way they can just use index zero instead of having to use the length of
	// the slice all the time.
	variables := slices.Clone(b.pathVariables)
	slices.Reverse(variables)

	// Create the paths parser:
	pathsParser, err := search.NewPathsParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}

	// Create the filter expression parser:
	selectorParser, err := search.NewSelectorParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create filter expression parser: %w", err)
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
	includePaths, err := pathsParser.Parse(b.includeFields...)
	if err != nil {
		return
	}
	excludePaths, err := pathsParser.Parse(b.excludeFields...)
	if err != nil {
		return
	}

	// Prepare the JSON iterator API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create and populate the object:
	result = &Adapter{
		logger:             b.logger,
		pathVariables:      variables,
		pathsParser:        pathsParser,
		collectionHandler:  collectionHandler,
		objectHandler:      objectHandler,
		includeFields:      includePaths,
		excludeFields:      excludePaths,
		selectorParser:     selectorParser,
		projectorEvaluator: projectorEvaluator,
		jsonAPI:            jsonAPI,
	}
	return
}

// Serve is the implementation of the http.Handler interface.
func (a *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get the values of the path variables:
	pathVariables := make([]string, len(a.pathVariables))
	muxVariables := mux.Vars(r)
	for i, name := range a.pathVariables {
		pathVariables[i] = muxVariables[name]
	}

	// If the handler only implements one of the interfaces then we use it unconditionally.
	// Otherwise we select the collection handler only if the first variable is empty.
	switch {
	case a.collectionHandler != nil && a.objectHandler == nil:
		a.serveCollection(w, r, pathVariables)
	case a.collectionHandler == nil && a.objectHandler != nil:
		a.serveObject(w, r, pathVariables)
	case pathVariables[0] == "":
		a.serveCollection(w, r, pathVariables[1:])
	default:
		a.serveObject(w, r, pathVariables)
	}
}

func (a *Adapter) serveObject(w http.ResponseWriter, r *http.Request, pathVariables []string) {
	// Get the context:
	ctx := r.Context()

	// Create the request:
	request := &GetRequest{
		Variables: pathVariables,
	}

	// Try to extract the projector:
	var ok bool
	request.Projector, ok = a.extractProjector(w, r)
	if !ok {
		return
	}

	// Call the handler:
	response, err := a.objectHandler.Get(ctx, request)
	if err != nil {
		a.logger.Error(
			"Failed to get object",
			"error", err,
		)
		if errors.Is(err, ErrNotFound) {
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

func (a *Adapter) serveCollection(w http.ResponseWriter, r *http.Request, pathVariables []string) {
	// Get the context:
	ctx := r.Context()

	// Create the request:
	request := &ListRequest{
		Variables: pathVariables,
	}

	// Try to extract the selector and projector:
	var ok bool
	request.Selector, ok = a.extractSelector(w, r)
	if !ok {
		return
	}
	request.Projector, ok = a.extractProjector(w, r)
	if !ok {
		return
	}

	// Call the handler:
	response, err := a.collectionHandler.List(ctx, request)
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

	// If there is a projector apply it:
	items := response.Items
	if request.Projector != nil {
		items = data.Map(
			items,
			func(ctx context.Context, item data.Object) (result data.Object, err error) {
				result, err = a.projectorEvaluator.Evaluate(ctx, request.Projector, item)
				return
			},
		)
	}

	a.sendItems(ctx, w, items)
}

// extractSelector tries to extract the selector from the request. It return the selector and a
// flag indicating if it is okay to continue processing the request. When this flag is false the
// error response was already sent to the client, and request processing should stop.
func (a *Adapter) extractSelector(w http.ResponseWriter,
	r *http.Request) (result *search.Selector, ok bool) {
	query := r.URL.Query()

	// Parse the filter:
	values, present := query["filter"]
	if present {
		for _, value := range values {
			selector, err := a.selectorParser.Parse(value)
			if err != nil {
				a.logger.Error(
					"Failed to parse filter expression",
					slog.String("filter", value),
					slog.String("error", err.Error()),
				)
				SendError(
					w,
					http.StatusBadRequest,
					"Failed to parse 'filter' parameter '%s': %v",
					value, err,
				)
				ok = false
				return
			}
			if result == nil {
				result = selector
			} else {
				result.Terms = append(result.Terms, selector.Terms...)
			}
		}
	}

	ok = true
	return
}

// extractProjector tries to extract the projector from the request. It return the selector and a
// flag indicating if it is okay to continue processing the request. When this flag is false the
// error response was already sent to the client, and request processing should stop.
func (a *Adapter) extractProjector(w http.ResponseWriter,
	r *http.Request) (result *search.Projector, ok bool) {
	query := r.URL.Query()

	// Parse the included fields:
	includeFields, present := query["fields"]
	if present {
		paths, err := a.pathsParser.Parse(includeFields...)
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
			ok = false
			return
		}
		if result == nil {
			result = &search.Projector{}
		}
		result.Include = paths
	} else if len(a.includeFields) > 0 {
		if result == nil {
			result = &search.Projector{}
		}
		result.Include = a.includeFields
	}

	// Parse the excluded fields:
	excludeFields, present := query["exclude_fields"]
	if present {
		paths, err := a.pathsParser.Parse(excludeFields...)
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
			ok = false
			return
		}
		if result == nil {
			result = &search.Projector{}
		}
		result.Exclude = paths
	} else if len(a.excludeFields) > 0 {
		if result == nil {
			result = &search.Projector{}
		}
		result.Exclude = a.excludeFields
	}

	ok = true
	return
}

func (a *Adapter) sendItems(ctx context.Context, w http.ResponseWriter,
	items data.Stream) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writer := jsoniter.NewStream(a.jsonAPI, w, 0)
	flusher, _ := w.(http.Flusher)
	a.writeItems(ctx, writer, flusher, items)
	err := writer.Flush()
	if err != nil {
		slog.Error(
			"Faild to flush stream",
			"error", err.Error(),
		)
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func (a *Adapter) writeItems(ctx context.Context, stream *jsoniter.Stream,
	flusher http.Flusher, items data.Stream) {
	i := 0
	stream.WriteArrayStart()
	for {
		item, err := items.Next(ctx)
		if err != nil {
			if !errors.Is(err, streaming.ErrEnd) {
				slog.Error(
					"Failed to get next item",
					"error", err.Error(),
				)
			}
			break
		}
		if i > 0 {
			stream.WriteMore()
		}
		stream.WriteVal(item)
		err = stream.Flush()
		if err != nil {
			slog.Error(
				"Faild to flush JSON stream",
				"error", err.Error(),
			)
		}
		if flusher != nil {
			flusher.Flush()
		}
		i++
	}
	stream.WriteArrayEnd()
}

func (a *Adapter) sendObject(ctx context.Context, w http.ResponseWriter,
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

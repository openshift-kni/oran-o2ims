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

// CollectionAdapterBuilder contains the data and logic needed to create collection adapters. Don't
// create instances of this type directly, use the NewCollectionAdapter function instead.
type CollectionAdapterBuilder struct {
	logger           *slog.Logger
	handler          CollectionHandler
	parentIdVariable string
	defaultInclude   []string
	defaultExclude   []string
}

// CollectionAdapter knows how to translate an HTTP request into a request for a collection of
// objects. Don't create instances of this type directly, use the NewCollectionAdapter function
// instead.
type CollectionAdapter struct {
	logger             *slog.Logger
	parentIdVariable   string
	pathsParser        *search.PathsParser
	selectorParser     *search.SelectorParser
	projectorEvaluator *search.ProjectorEvaluator
	defaultInclude     []search.Path
	defaultExclude     []search.Path
	jsonAPI            jsoniter.API
	handler            CollectionHandler
}

// NewCollectionAdapter creates a builder that can be used to configure and create a collection
// adatper.
func NewCollectionAdapter() *CollectionAdapterBuilder {
	return &CollectionAdapterBuilder{}
}

// SetLogger sets the logger that the server will use to write to the log. This is mandatory.
func (b *CollectionAdapterBuilder) SetLogger(logger *slog.Logger) *CollectionAdapterBuilder {
	b.logger = logger
	return b
}

// SetHandler sets the object that will handle the requests. This is mandatory.
func (b *CollectionAdapterBuilder) SetHandler(value CollectionHandler) *CollectionAdapterBuilder {
	b.handler = value
	return b
}

// SetParentIDVariable sets the name of the path variable that contains the identifier of the collection.
// This is optional. If not specified then no identifier will be passed to the handler.
func (b *CollectionAdapterBuilder) SetParentIDVariable(value string) *CollectionAdapterBuilder {
	b.parentIdVariable = value
	return b
}

// SetDefaultInclude set the collection of fields that will be included by default from the
// response. Each field is specified using the same syntax accepted by paths parsers.
func (b *CollectionAdapterBuilder) SetDefaultInclude(values ...string) *CollectionAdapterBuilder {
	b.defaultInclude = append(b.defaultExclude, values...)
	return b
}

// SetDefaultExclude sets the collection of fields that will be excluded by default from the
// response. Each field is specified using the same syntax accepted by paths parsers.
func (b *CollectionAdapterBuilder) SetDefaultExclude(values ...string) *CollectionAdapterBuilder {
	b.defaultExclude = append(b.defaultExclude, values...)
	return b
}

// Build uses the data stored in the builder to create and configure a new adapter.
func (b *CollectionAdapterBuilder) Build() (result *CollectionAdapter, err error) {
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
	result = &CollectionAdapter{
		logger:             b.logger,
		pathsParser:        pathsParser,
		selectorParser:     selectorParser,
		projectorEvaluator: projectorEvaluator,
		defaultInclude:     defaultInclude,
		defaultExclude:     defaultExclude,
		handler:            b.handler,
		parentIdVariable:   b.parentIdVariable,
		jsonAPI:            jsonAPI,
	}
	return
}

// Serve is the implementation of the http.Handler interface.
func (a *CollectionAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error

	// Get the context:
	ctx := r.Context()

	// Get the query parameters:
	query := r.URL.Query()

	// Create the request:
	request := &ListRequest{
		ParentID: mux.Vars(r)[a.parentIdVariable],
		Projector: &search.Projector{
			Include: a.defaultInclude,
			Exclude: a.defaultExclude,
		},
	}

	// Check if there is a selector, and parse it:
	values, ok := query["filter"]
	if ok {
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
					"Failed to parse filter expression '%s': %v",
					value, err,
				)
				return
			}
			a.logger.Debug(
				"Parsed filter expressions",
				slog.String("source", value),
				slog.String("parsed", selector.String()),
			)
			if request.Selector == nil {
				request.Selector = selector
			} else {
				request.Selector.Terms = append(request.Selector.Terms, selector.Terms...)
			}
		}
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
	response, err := a.handler.List(ctx, request)
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

func (a *CollectionAdapter) sendItems(ctx context.Context, w http.ResponseWriter,
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

func (a *CollectionAdapter) writeItems(ctx context.Context, stream *jsoniter.Stream,
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

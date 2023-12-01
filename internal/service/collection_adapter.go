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
	"github.com/openshift-kni/oran-o2ims/internal/streaming"
)

type CollectionAdapterBuilder struct {
	logger           *slog.Logger
	handler          CollectionHandler
	parentIdVariable string
}

type CollectionAdapter struct {
	logger             *slog.Logger
	parentIdVariable   string
	selectorParser     *search.SelectorParser
	projectorParser    *search.ProjectorParser
	projectorEvaluator *search.ProjectorEvaluator
	jsonAPI            jsoniter.API
	handler            CollectionHandler
}

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

	// Create the filter expression parser:
	selectorParser, err := search.NewSelectorParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create filter expression parser: %w", err)
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
	result = &CollectionAdapter{
		logger:             b.logger,
		selectorParser:     selectorParser,
		projectorParser:    projectorParser,
		projectorEvaluator: projectorEvaluator,
		handler:            b.handler,
		parentIdVariable:   b.parentIdVariable,
		jsonAPI:            jsonAPI,
	}
	return
}

// Serve is the implementation of the http.Handler interface.
func (a *CollectionAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	request := &ListRequest{
		ParentID: mux.Vars(r)[a.parentIdVariable],
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
	values, ok = query["fields"]
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
			a.logger.Info(
				"Parsed field selector",
				slog.String("source", value),
				slog.Any("parsed", projector),
			)
			request.Projector = append(request.Projector, projector...)
		}
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

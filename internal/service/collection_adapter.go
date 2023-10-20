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

	"github.com/jhernand/o2ims/internal/data"
	"github.com/jhernand/o2ims/internal/filter"
	"github.com/jhernand/o2ims/internal/streaming"
	jsoniter "github.com/json-iterator/go"
)

type CollectionAdapterBuilder struct {
	logger  *slog.Logger
	handler CollectionHandler
}

type CollectionAdapter struct {
	logger       *slog.Logger
	filterParser *filter.Parser
	jsonAPI      jsoniter.API
	handler      CollectionHandler
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
	parser, err := filter.NewParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create filter expression parser: %w", err)
		return
	}

	// Prepare the JSON iterator API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create and populate the object:
	result = &CollectionAdapter{
		logger:       b.logger,
		filterParser: parser,
		handler:      b.handler,
		jsonAPI:      jsonAPI,
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
	query := r.URL.Query()

	// Get the context:
	ctx := r.Context()

	// Create the request:
	request := &CollectionRequest{}

	// Check if there is a filter expression, and parse it:
	values, ok := query["filter"]
	if ok {
		for _, value := range values {
			expr, err := a.filterParser.Parse(value)
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
			a.logger.Info(
				"Parsed filter expressions",
				slog.String("source", value),
				slog.String("parsed", expr.String()),
			)
			if request.Filter == nil {
				request.Filter = expr
			} else {
				request.Filter.Terms = append(request.Filter.Terms, expr.Terms...)
			}
		}
	}

	// Call the handler:
	switch r.Method {
	case http.MethodGet:
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
		a.sendItems(ctx, w, response.Items)
		return
	default:
		SendError(
			w,
			http.StatusMethodNotAllowed,
			"Method '%s' is not allowed",
			r.Method,
		)
		return
	}
}

func (a *CollectionAdapter) sendItems(ctx context.Context, w http.ResponseWriter,
	items data.Stream) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writer := jsoniter.NewStream(a.jsonAPI, w, 0)
	a.writeItems(ctx, writer, items)
	err := writer.Flush()
	if err != nil {
		slog.Error(
			"Faild to flush JSON stream",
			"error", err.Error(),
		)
	}
}

func (a *CollectionAdapter) writeItems(ctx context.Context, writer *jsoniter.Stream,
	items data.Stream) {
	i := 0
	writer.WriteArrayStart()
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
			writer.WriteMore()
		}
		writer.WriteVal(item)
		err = writer.Flush()
		if err != nil {
			slog.Error(
				"Faild to flush JSON stream",
				"error", err.Error(),
			)
		}
		i++
	}
	writer.WriteArrayEnd()
}

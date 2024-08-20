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
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	neturl "net/url"
	"slices"
	"strings"

	"github.com/gorilla/mux"
	jsoniter "github.com/json-iterator/go"
	"github.com/peterhellberg/link"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/search"
	"github.com/openshift-kni/oran-o2ims/internal/streaming"
)

// AdapterBuilder contains the data and logic needed to create adapters. Don't create instances of
// this type directly, use the NewAdapter function instead.
type AdapterBuilder struct {
	logger              *slog.Logger
	pathVariables       []string
	handler             any
	includeFields       []string
	excludeFields       []string
	externalAddress     string
	nextPageMarkerKey   []byte
	nextPageMarkerNonce []byte
}

// Adapter knows how to translate an HTTP request into a request for a collection of objects. Don't
// create instances of this type directly, use the NewAdapter function instead.
type Adapter struct {
	logger               *slog.Logger
	pathVariables        []string
	listHandler          ListHandler
	getHandler           GetHandler
	addHandler           AddHandler
	deleteHandler        DeleteHandler
	includeFields        []search.Path
	excludeFields        []search.Path
	pathsParser          *search.PathsParser
	selectorParser       *search.SelectorParser
	selectorEvaluator    *search.SelectorEvaluator
	projectorEvaluator   *search.ProjectorEvaluator
	externalAddress      *neturl.URL
	nextPageMarkerCipher cipher.AEAD
	nextPageMarkerNonce  []byte
	jsonAPI              jsoniter.API
}

// NewAdapter creates a builder that can be used to configure and create an adatper.
func NewAdapter() *AdapterBuilder {
	return &AdapterBuilder{
		nextPageMarkerKey: slices.Clone(defaultAdapdterNextPageMarkerKey[:]),
	}
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

// SetExternalAddress set the URL of the service as seen by external users.
func (b *AdapterBuilder) SetExternalAddress(value string) *AdapterBuilder {
	b.externalAddress = value
	return b
}

// SetNextPageMarkerKey sets the key that is used to encrypt and decrypt the next page opaque
// tokens. The purpose of this encryption is to discourage clients from assuming the format of the
// marker. By default a hard coded key is used. That key is effectively public because it is part
// of the source code. There is usually no reason to change it, but you can use this method if you
// really need.
func (b *AdapterBuilder) SetNextPageMarkerKey(value []byte) *AdapterBuilder {
	b.nextPageMarkerKey = slices.Clone(value)
	return b
}

// SetNextPageMarkerNonce sets the nonce that is used to encrypt and decrypt the next page opaque
// tokens. The purpose of this encryption is to discourage clients from assuming the format of the
// marker. By default a random nonce is usually each time that a marker is encrypted, but for unit
// tests it is convenient to be able to explicitly specify it, so that the resulting encrypted
// marker will be predictable.
func (b *AdapterBuilder) SetNextPageMarkerNonce(value []byte) *AdapterBuilder {
	b.nextPageMarkerNonce = slices.Clone(value)
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
	if b.nextPageMarkerKey == nil {
		err = errors.New("next page marker key is mandatory")
		return
	}

	// Parse the external address once, so that when we need to use it later we only
	// need to copy it.
	var externalAddress *neturl.URL
	if b.externalAddress != "" {
		externalAddress, err = neturl.Parse(b.externalAddress)
		if err != nil {
			return
		}
	}

	// Check that the handler implements at least one of the handler interfaces:
	listHandler, _ := b.handler.(ListHandler)
	getHandler, _ := b.handler.(GetHandler)
	addHandler, _ := b.handler.(AddHandler)
	deleteHandler, _ := b.handler.(DeleteHandler)
	if listHandler == nil && getHandler == nil && addHandler == nil && deleteHandler == nil {
		err = errors.New("handler doesn't implement any of the handler interfaces")
		return
	}

	// If the handler implements the list and get handler interfaces then we need to have at
	// least one path variable because we use it to decide if the request is for the collection
	// or for a specific object.
	if listHandler != nil && getHandler != nil && len(b.pathVariables) == 0 {
		err = errors.New(
			"at least one path variable is required when both the list and " +
				"get handlers are implemented",
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

	// Create the selector parser:
	selectorParser, err := search.NewSelectorParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create selector parser: %w", err)
		return
	}

	// Create the path evaluator:
	pathEvaluator, err := search.NewPathEvaluator().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create projector path evaluator: %w", err)
		return
	}

	// Create the selector evaluator:
	selectorEvaluator, err := search.NewSelectorEvaluator().
		SetLogger(b.logger).
		SetPathEvaluator(pathEvaluator.Evaluate).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create the selector evaluator: %w", err)
		return
	}

	// Create the projector evaluator:
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

	// Create the cipher for the next page markers:
	nextPageMarkerCipher, err := b.createNextPageMarkerCipher()
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
		logger:               b.logger,
		pathVariables:        variables,
		pathsParser:          pathsParser,
		listHandler:          listHandler,
		getHandler:           getHandler,
		addHandler:           addHandler,
		deleteHandler:        deleteHandler,
		includeFields:        includePaths,
		excludeFields:        excludePaths,
		selectorParser:       selectorParser,
		selectorEvaluator:    selectorEvaluator,
		projectorEvaluator:   projectorEvaluator,
		externalAddress:      externalAddress,
		nextPageMarkerCipher: nextPageMarkerCipher,
		nextPageMarkerNonce:  slices.Clone(b.nextPageMarkerNonce),
		jsonAPI:              jsonAPI,
	}
	return
}

// createNextPageMarkerCipher creates the cipher that will be used to encrypt the next page
// markers. Currently this creates an AES cipher with Galois counter mode.
func (b *AdapterBuilder) createNextPageMarkerCipher() (result cipher.AEAD, err error) {
	// Create the cipher:
	blockCipher, err := aes.NewCipher(b.nextPageMarkerKey)
	if err != nil {
		return
	}
	gcmCipher, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return
	}

	// If the nonce has been explicitly specified then check that it has the required size:
	if b.nextPageMarkerNonce != nil {
		requiredSize := gcmCipher.NonceSize()
		actualSize := len(b.nextPageMarkerNonce)
		if actualSize != requiredSize {
			err = fmt.Errorf(
				"nonce has been explicitly specified, and it is %d bytes long, "+
					"but the cipher requires %d",
				actualSize, requiredSize,
			)
			return
		}
	}

	// Return the cipher:
	result = gcmCipher
	return
}

// Serve is the implementation of the http.Handler interface.
func (a *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get the context:
	ctx := r.Context()

	// Get the values of the path variables:
	pathVariables := make([]string, len(a.pathVariables))
	muxVariables := mux.Vars(r)
	for i, name := range a.pathVariables {
		pathVariables[i] = muxVariables[name]
	}

	// Serve according to the HTTP method:
	switch r.Method {
	case http.MethodGet:
		a.serveGetMethod(ctx, w, r, pathVariables)
	case http.MethodPost:
		a.servePostMethod(ctx, w, r, pathVariables)
	case http.MethodDelete:
		a.serveDeleteMethod(ctx, w, r, pathVariables)
	default:
		SendError(w, http.StatusMethodNotAllowed, "Method '%s' is not allowed", r.Method)
	}
}

func (a *Adapter) serveGetMethod(ctx context.Context, w http.ResponseWriter, r *http.Request,
	pathVariables []string) {
	// Check that we have a compatible handler:
	if a.listHandler == nil && a.getHandler == nil {
		SendError(w, http.StatusMethodNotAllowed, "Method '%s' is not allowed", r.Method)
		return
	}

	// If the handler only implements one of the interfaces then we use it unconditionally.
	// Otherwise we select the collection handler only if the first variable is empty.
	switch {
	case a.listHandler != nil && a.getHandler == nil:
		a.serveList(ctx, w, r, pathVariables)
	case a.listHandler == nil && a.getHandler != nil:
		a.serveGet(ctx, w, r, pathVariables)
	case pathVariables[0] == "":
		a.serveList(ctx, w, r, pathVariables[1:])
	default:
		a.serveGet(ctx, w, r, pathVariables)
	}
}

func (a *Adapter) servePostMethod(ctx context.Context, w http.ResponseWriter, r *http.Request,
	pathVariables []string) {
	// Check that we have a compatible handler:
	if a.addHandler == nil {
		SendError(w, http.StatusMethodNotAllowed, "Method '%s' is not allowed", r.Method)
		return
	}

	// Call the handler:
	a.serveAdd(w, r, pathVariables)
}

func (a *Adapter) serveDeleteMethod(ctx context.Context, w http.ResponseWriter, r *http.Request,
	pathVariables []string) {
	// Check that we have a compatible handler:
	if a.deleteHandler == nil {
		SendError(w, http.StatusMethodNotAllowed, "Method '%s' is not allowed", r.Method)
		return
	}

	// Call the handler:
	a.serveDelete(ctx, w, r, pathVariables)
}

func (a *Adapter) serveGet(ctx context.Context, w http.ResponseWriter, r *http.Request,
	pathVariables []string) {
	// Create the request:
	request := &GetRequest{
		Variables: pathVariables,
	}

	// Try to extract the projector:
	var ok bool
	request.Projector, ok = a.extractProjector(ctx, w, r)
	if !ok {
		return
	}

	// Call the handler:
	response, err := a.getHandler.Get(ctx, request)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to get object",
			"error", err,
		)
		if errors.Is(err, ErrNotFound) || errors.Is(err, streaming.ErrEnd) {
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
			a.logger.ErrorContext(
				ctx,
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

func (a *Adapter) serveList(ctx context.Context, w http.ResponseWriter, r *http.Request,
	pathVariables []string) {
	// Create the request:
	request := &ListRequest{
		Variables: pathVariables,
	}

	// Try to extract, projector and next page marker:
	var ok bool
	request.Selector, ok = a.extractSelector(ctx, w, r)
	if !ok {
		return
	}
	request.Projector, ok = a.extractProjector(ctx, w, r)
	if !ok {
		return
	}
	request.NextPageMarker, ok = a.extractNextPageMarker(ctx, w, r)
	if !ok {
		return
	}

	// Call the handler:
	response, err := a.listHandler.List(ctx, request)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
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

	// If there is a selector apply it:
	items := response.Items
	if request.Selector != nil {
		items = data.Select(
			items,
			func(ctx context.Context, item data.Object) (result bool, err error) {
				result, err = a.selectorEvaluator.Evaluate(ctx, request.Selector, item)
				return
			},
		)
	}

	// If there is a projector apply it:
	if request.Projector != nil {
		items = data.Map(
			items,
			func(ctx context.Context, item data.Object) (result data.Object, err error) {
				result, err = a.projectorEvaluator.Evaluate(ctx, request.Projector, item)
				return
			},
		)
	}

	// Set the next page marker:
	ok = a.addNextPageMarker(w, r, response.NextPageMarker)
	if !ok {
		return
	}

	a.sendItems(ctx, w, items)
}

func (a *Adapter) serveAdd(w http.ResponseWriter, r *http.Request, pathVariables []string) {
	// Get the context:
	ctx := r.Context()

	// Check that the content type is acceptable:
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		a.logger.ErrorContext(
			ctx,
			"Received empty content type header",
		)
		SendError(
			w, http.StatusBadRequest,
			"Content type is mandatory, use 'application/json'",
		)
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to parse content type",
			slog.String("header", contentType),
			slog.String("error", err.Error()),
		)
		SendError(w, http.StatusBadRequest, "Failed to parse content type '%s'", contentType)
	}
	if !strings.EqualFold(mediaType, "application/json") {
		a.logger.ErrorContext(
			ctx,
			"Unsupported content type",
			slog.String("header", contentType),
			slog.String("media", mediaType),
		)
		SendError(
			w, http.StatusBadRequest,
			"Content type '%s' isn't supported, use 'application/json'",
			mediaType,
		)
		return
	}

	// Parse the request body:
	decoder := a.jsonAPI.NewDecoder(r.Body)
	var object data.Object
	err = decoder.Decode(&object)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to decode input",
			slog.String("error", err.Error()),
		)
		SendError(w, http.StatusBadRequest, "Failed to decode input")
		return
	}

	// Create the request:
	request := &AddRequest{
		Variables: pathVariables,
		Object:    object,
	}

	// Call the handler:
	response, err := a.addHandler.Add(ctx, request)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to add item",
			"error", err,
		)
		SendError(
			w,
			http.StatusInternalServerError,
			"Failed to add item",
		)
		return
	}

	// Send the added object:
	a.sendObject(ctx, w, response.Object)
}

func (a *Adapter) serveDelete(ctx context.Context, w http.ResponseWriter, r *http.Request,
	pathVariables []string) {
	// Create the request:
	request := &DeleteRequest{
		Variables: pathVariables,
	}

	// Call the handler:
	_, err := a.deleteHandler.Delete(ctx, request)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to delete item",
			"error", err,
		)
		SendError(
			w,
			http.StatusInternalServerError,
			"Failed to delete item",
		)
		return
	}

	// Send the result:
	w.WriteHeader(http.StatusNoContent)
}

// extractSelector tries to extract the selector from the request. It return the selector and a
// flag indicating if it is okay to continue processing the request. When this flag is false the
// error response was already sent to the client, and request processing should stop.
func (a *Adapter) extractSelector(ctx context.Context, w http.ResponseWriter,
	r *http.Request) (result *search.Selector, ok bool) {
	query := r.URL.Query()

	// Parse the filter:
	values, present := query["filter"]
	if present {
		for _, value := range values {
			selector, err := a.selectorParser.Parse(value)
			if err != nil {
				a.logger.ErrorContext(
					ctx,
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
func (a *Adapter) extractProjector(ctx context.Context, w http.ResponseWriter,
	r *http.Request) (result *search.Projector, ok bool) {
	query := r.URL.Query()

	// Parse the included fields:
	includeFields, present := query["fields"]
	if present {
		paths, err := a.pathsParser.Parse(includeFields...)
		if err != nil {
			a.logger.ErrorContext(
				ctx,
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
			a.logger.ErrorContext(
				ctx,
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

// extractNextPageMarker tries to extract the next page marker from the `nextpage_opaque_marker`
// query parameter. It returns the marker and a flag indicating if it is okay to continue
// processing the request. When this flag is false the error response was already sent to the
// client, and request processing should stop.
func (a *Adapter) extractNextPageMarker(ctx context.Context, w http.ResponseWriter,
	r *http.Request) (result []byte, ok bool) {
	// Get the marker text:
	text := r.URL.Query().Get(adapterNextPageOpaqueMarkerQueryParameterName)
	if text == "" {
		ok = true
		return
	}

	// Decrypt the marker:
	data, err := a.decryptNextPageMarker(text)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to decrypt next page marker",
			slog.String("text", text),
			slog.String("error", err.Error()),
		)
		SendError(
			w,
			http.StatusBadRequest,
			"Failed to decrypt next page marker '%s'",
			text,
		)
		ok = false
		return
	}

	// Return the value:
	result = data
	ok = true
	return
}

// setNextPageMarker generates the next page marker data and adds it to the response header as a
// link. It returns a flag indicating if it is okay to continue processing the request. When this
// flag is false the error response has already been sent to the client, and the request processing
// should stop.
func (a *Adapter) addNextPageMarker(w http.ResponseWriter, r *http.Request, value []byte) bool {
	// Get the context:
	ctx := r.Context()

	// Do nothing if there is no marker:
	if value == nil {
		return true
	}

	// Encrypt the marker:
	text, err := a.encryptNextPageMarker(value)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to encrypt next page marker",
			slog.Any("value", value),
			slog.String("error", err.Error()),
		)
		SendError(
			w,
			http.StatusInternalServerError,
			"Failed to encrypt next page marker",
		)
		return false
	}

	// Generate the complete link, preserving existing query parameters and replacing any
	// possible existing next page marker with the one that we just generated.
	url := *r.URL
	if a.externalAddress != nil {
		url.Scheme = a.externalAddress.Scheme
		url.Host = a.externalAddress.Host
	}
	url.Path = r.URL.Path
	query := neturl.Values{}
	for name, values := range r.URL.Query() {
		if name != adapterNextPageOpaqueMarkerQueryParameterName {
			query[name] = slices.Clone(values)
		}
	}
	query.Set(adapterNextPageOpaqueMarkerQueryParameterName, text)
	url.RawQuery = query.Encode()

	// Add the link to the header:
	header := w.Header()
	links := link.ParseHeader(header)
	if links == nil {
		links = link.Group{}
	}
	next, ok := links["next"]
	if ok {
		a.logger.WarnContext(
			r.Context(),
			"Next page link is already set, will replace it",
			"link", next.String(),
			"text", text,
		)
	}
	links["next"] = &link.Link{
		URI: url.String(),
		Rel: "next",
	}
	buffer := &bytes.Buffer{}
	i := 0
	for _, current := range links {
		if i > 0 {
			buffer.WriteString(", ")
		}
		fmt.Fprintf(buffer, `<%s>; rel="%s"`, current.URI, current.Rel)
		for extraName, extraValue := range current.Extra {
			buffer.WriteString(";")
			fmt.Fprintf(buffer, `%s="%s"`, extraName, extraValue)
		}
		i++
	}
	header.Set("Link", buffer.String())
	return true
}

func (a *Adapter) encryptNextPageMarker(value []byte) (result string, err error) {
	// If a nonce has been explicitly provided then use it, otherwise generate a random one:
	var nonce []byte
	if a.nextPageMarkerNonce != nil {
		nonce = a.nextPageMarkerNonce
	} else {
		nonce = make([]byte, a.nextPageMarkerCipher.NonceSize())
		_, err = rand.Read(nonce)
		if err != nil {
			return
		}
	}

	// Encrypt the marker:
	data := a.nextPageMarkerCipher.Seal(nil, nonce, value, nil)

	// We will need the nonce to decrypt, so the marker will contain the result of
	// concatenating it with the encrypted data.
	data = append(nonce, data...)

	// Encode the as text safe for use in a query parameter:
	result = base64.RawURLEncoding.EncodeToString(data)
	return
}

func (a *Adapter) decryptNextPageMarker(text string) (result []byte, err error) {
	// Decode the text:
	data, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return
	}

	// The data should contain the nonce contatenated with the encrypted data, so it needs
	// to be at least as long as the nonce size required by the cipher.
	dataSize := len(data)
	nonceSize := a.nextPageMarkerCipher.NonceSize()
	if dataSize < nonceSize {
		err = fmt.Errorf(
			"marker size (%d bytes) is smaller than nonce size (%d bytes)",
			dataSize, nonceSize,
		)
		return
	}

	// Separate the nonce and the encrypted data:
	nonce := data[0:nonceSize]
	data = data[nonceSize:]

	// Decrypt the data:
	result, err = a.nextPageMarkerCipher.Open(nil, nonce, data, nil)
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
		slog.ErrorContext(
			ctx,
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
				slog.ErrorContext(
					ctx,
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
			slog.ErrorContext(
				ctx,
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
		a.logger.ErrorContext(
			ctx,
			"Failed to send object",
			"error", writer.Error.Error(),
		)
	}
	writer.Flush()
	if writer.Error != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to flush stream",
			"error", writer.Error.Error(),
		)
	}
}

// Names of query parameters:
const (
	adapterNextPageOpaqueMarkerQueryParameterName = "nextpage_opaque_marker"
)

// defaultAdapterNextPageMarkerKey is the key used to encrypt and decrypt next page markers by
// default. It is OK to have it in clear text here because its only purpose is to discourage
// users from messing with the contents.
var defaultAdapdterNextPageMarkerKey = [...]byte{
	0x44, 0x48, 0xb5, 0x2c, 0xa5, 0x11, 0x4e, 0xea,
	0x7e, 0x37, 0xcf, 0x85, 0x34, 0x5b, 0x5f, 0xd7,
	0x04, 0xc7, 0x37, 0x3a, 0x5b, 0x9f, 0x49, 0x63,
	0x54, 0xa9, 0xb0, 0xbd, 0x2d, 0x5e, 0xcc, 0x37,
}

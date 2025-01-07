package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/search"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

// Query parameters definitions
const (
	fields        = "fields"
	excludeFields = "exclude_fields"
	filter        = "filter"
)

// FilterAdapter is an abstraction that wraps the search projector/selector functionality so that
// these objects can be created once at server initialization time and re-used in the ResponseFilter
// middleware.
type FilterAdapter struct {
	swagger            *openapi3.T
	router             routers.Router
	pathsParser        *search.PathsParser
	projectorEvaluator *search.ProjectorEvaluator
	selectorEvaluator  *search.SelectorEvaluator
	selectorParser     *search.SelectorParser
}

// NewFilterAdapter creates a new filter adapter to be passed to a ResponseFilter
func NewFilterAdapter(logger *slog.Logger, swagger *openapi3.T) (*FilterAdapter, error) {
	pathsParser, err := search.NewPathsParser().SetLogger(logger).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build paths parser: %w", err)
	}

	pathEvaluator, err := search.NewPathEvaluator().SetLogger(logger).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build path evaluator: %w", err)
	}

	selectorParser, err := search.NewSelectorParser().
		SetLogger(logger).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build selector parser: %w", err)
	}

	selectorEvaluator, err := search.NewSelectorEvaluator().
		SetLogger(logger).
		SetPathEvaluator(pathEvaluator.Evaluate).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build selector evaluator: %w", err)
	}

	projectEvaluator, err := search.NewProjectorEvaluator().
		SetLogger(logger).
		SetPathEvaluator(pathEvaluator.Evaluate).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build projector evaluator: %w", err)
	}

	// We don't want the host to be considered
	swagger.Servers = nil
	router, err := gorillamux.NewRouter(swagger)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	return &FilterAdapter{
		swagger:            swagger,
		router:             router,
		pathsParser:        pathsParser,
		projectorEvaluator: projectEvaluator,
		selectorEvaluator:  selectorEvaluator,
		selectorParser:     selectorParser,
	}, nil
}

// ParseFields delegates the function of parsing the include/exclude fields to the path parser.
func (a *FilterAdapter) ParseFields(fields ...string) ([]search.Path, error) {
	paths, err := a.pathsParser.Parse(fields...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse fields: %w", err)
	}
	return paths, nil
}

// EnforceRequiredFields ensures that required fields are always included and never excluded
func (a *FilterAdapter) EnforceRequiredFields(projector *search.Projector, r *http.Request) error {
	requiredFields, err := a.getRequiredFields(r)
	if err != nil {
		return err
	}
	if len(projector.Include) > 0 {
		// If any fields are explicitly included then make sure that they are a superset of the required fields.
		projector.Include = append(projector.Include, requiredFields...)
	}
	excludedFields := make([]search.Path, 0)
	for _, field := range projector.Exclude {
		found := false
		for _, required := range requiredFields {
			if slices.Equal(field, required) {
				found = true
				break
			}
		}
		if !found {
			// Only allow fields to be excluded if they are not in the required list.
			excludedFields = append(excludedFields, field)
		}
	}
	projector.Exclude = excludedFields
	return nil
}

// ParseFilter delegates the function of parsing the filter fields to the selector parser.
func (a *FilterAdapter) ParseFilter(query string) (*search.Selector, error) {
	selector, err := a.selectorParser.Parse(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse filter: %w", err)
	}
	return selector, nil
}

// getOperation attempts to lookup the OpenAPI Operation for the given request
func (a *FilterAdapter) getOperation(r *http.Request) (*openapi3.Operation, error) {
	route, _, err := a.router.FindRoute(r)
	if err != nil {
		return nil, fmt.Errorf("failed to find route: %w", err)
	}

	operation := route.Operation
	if operation == nil {
		return nil, fmt.Errorf("failed to find operation")
	}

	return operation, nil
}

// getResponseSchema attempts to lookup the OpenAPI Schema for the given request
func (a *FilterAdapter) getResponseSchema(operation *openapi3.Operation) (*openapi3.Schema, error) {
	responses := operation.Responses
	if responses == nil {
		return nil, fmt.Errorf("failed to find responses")
	}

	// We currently only do filtering on successful responses so we force this to be 200
	responseRef := responses.Value(strconv.Itoa(http.StatusOK))
	if responseRef == nil {
		return nil, fmt.Errorf("failed to find response reference")
	}

	response := responseRef.Value
	if response == nil {
		return nil, fmt.Errorf("failed to find response")
	}

	content := response.Content.Get("application/json")
	if content == nil {
		return nil, fmt.Errorf("failed to find content")
	}

	schemaRef := content.Schema
	if schemaRef == nil {
		return nil, fmt.Errorf("failed to find schema reference")
	}

	schema := schemaRef.Value
	if schema == nil {
		return nil, fmt.Errorf("failed to find schema")
	}

	return schema, nil
}

// makePaths converts an array of strings to an array of Path objects
func (a *FilterAdapter) makePaths(fields []string) []search.Path {
	paths := make([]search.Path, len(fields))
	for i, field := range fields {
		paths[i] = []string{field}
	}
	return paths
}

// getRequiredFields attempts to build the list of fields required in the response for the given request.  The required
// fields are extracted from the OpenAPI response schema.
func (a *FilterAdapter) getRequiredFields(r *http.Request) ([]search.Path, error) {
	operation, err := a.getOperation(r)
	if err != nil {
		return []search.Path{}, err
	}

	schema, err := a.getResponseSchema(operation)
	if err != nil {
		return []search.Path{}, err
	}

	schemaTypes := schema.Type
	if schemaTypes == nil {
		return []search.Path{}, fmt.Errorf("failed to find schema")
	}

	if schemaTypes.Includes("array") {
		itemsRef := schema.Items
		if itemsRef == nil {
			return []search.Path{}, fmt.Errorf("failed to find items reference for array schema")
		}

		items := itemsRef.Value
		if items == nil {
			return []search.Path{}, fmt.Errorf("failed to find items for array schema")
		}

		return a.makePaths(items.Required), nil
	}

	if schemaTypes.Includes("object") {
		return a.makePaths(schema.Required), nil
	}

	// We don't currently support any responses other than object or array

	return []search.Path{}, nil
}

// EvaluateSelector delegates the function of evaluating the set of search selectors to the selector evaluator.
func (a *FilterAdapter) EvaluateSelector(selector *search.Selector, object any) (bool, error) {
	result, err := a.selectorEvaluator.Evaluate(context.TODO(), selector, object)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate selector: %w", err)
	}
	return result, nil
}

// EvaluateProjector delegates the function of evaluating the set of field projections to the projector evaluator.
func (a *FilterAdapter) EvaluateProjector(projector *search.Projector,
	object any) (map[string]any, error) {
	result, err := a.projectorEvaluator.Evaluate(context.TODO(), projector, object)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate projector: %w", err)
	}
	return result, nil
}

// Error sends an error using the proper ORAN format
func (a *FilterAdapter) Error(w http.ResponseWriter, details string, status int) error {
	out, _ := json.Marshal(common.ProblemDetails{
		Detail: details,
		Status: status,
	})
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, err := w.Write(out)
	if err != nil {
		return fmt.Errorf("failed to write response error: %w", err)
	}
	return nil
}

// FilterResponseInterceptor implements the http.ResponseWriter interface so that it can be used to
// intercept all operations intended for the request's ResponseWriter into a local buffer.  At the
// end of the request the local buffer is evaluated against the selector/projector built from the
// 'fields', 'exclude_fields', and 'filter' query parameters and transforms the response object
// accordingly.
type FilterResponseInterceptor struct {
	adapter    *FilterAdapter
	original   http.ResponseWriter
	buf        bytes.Buffer
	projector  *search.Projector
	selector   *search.Selector
	statusCode int
}

// Header is a simple pass-through to the original http.ResponseWriter's Header method
func (i *FilterResponseInterceptor) Header() http.Header {
	return i.original.Header()
}

// WriteHeader intercepts the response's status code and stores it locally.  It is not passed
// through in case processing in this interceptor fails, and we need to override the response code.
func (i *FilterResponseInterceptor) WriteHeader(statusCode int) {
	i.statusCode = statusCode
}

// Write intercepts the bytes intended for the underlying http.ResponseWriter and stores them locally
// for later processing.
func (i *FilterResponseInterceptor) Write(data []byte) (int, error) {
	count, err := i.buf.Write(data)
	if err != nil {
		return count, fmt.Errorf("failed to write response: %w", err)
	}
	return count, nil
}

// Flush is invoked at the end of the request so that the response can be transformed/filtered if
// necessary.  Both the selector (filtering) and projector (transformations) are applied to any
// operations that have a 200 status code and contain valid JSON for either a list or object
// representation.
func (i *FilterResponseInterceptor) Flush(r *http.Request) error {
	if i.statusCode != 200 || (i.projector.Empty() && len(i.selector.Terms) == 0) {
		// We're only interested in GET requests for lists and objects when there are filters
		// provided and only on successful requests; therefore for all other combinations we can
		// simply ignore.
		if i.statusCode > 0 {
			// Propagate the status code to the original response writer
			i.original.WriteHeader(i.statusCode)
		}
		_, err := i.original.Write(i.buf.Bytes())
		if err != nil {
			return fmt.Errorf("failed to write bytes: %w", err)
		}
		return nil
	}

	content := i.buf.Bytes()
	var objectResult data.Object
	// We don't have any context about whether this is a List or Get request so we have to look at
	// the response and try to guess.  Here we attempt to unmarshall as an object and if that
	// doesn't work then we try again as a list.  One of these two attempts should succeed.
	if err := json.Unmarshal(content, &objectResult); err != nil {
		var listResult []data.Object
		if err = json.Unmarshal(content, &listResult); err != nil {
			return fmt.Errorf("unable to unmarshal response as either list or object")
		}

		items := make([]data.Object, 0)
		if len(i.selector.Terms) > 0 {
			// Apply the selector to reduce the list of items down to only those of interest to the caller.
			for _, item := range listResult {
				ok, err := i.adapter.EvaluateSelector(i.selector, item)
				if err != nil {
					// Not likely a 500 error so send a 400 and return nil instead
					return i.adapter.Error(i.original, err.Error(), http.StatusBadRequest)
				}
				if ok {
					items = append(items, item)
				}
			}
		} else {
			items = listResult
		}

		if !i.projector.Empty() {
			// Apply the projector to reduce the attributes included in each item down to only those of interest to the
			// caller.  Mandatory fields cannot be excluded so we force them to be included.
			for index, item := range items {
				mappedItem, err := i.adapter.EvaluateProjector(i.projector, item)
				if err != nil {
					// Not likely a 500 error so send a 400 and return nil instead
					return i.adapter.Error(i.original, err.Error(), http.StatusBadRequest)
				}
				items[index] = mappedItem
			}
		}

		i.original.WriteHeader(i.statusCode)
		err = json.NewEncoder(i.original).Encode(items)
		if err != nil {
			return fmt.Errorf("failed to encode list: %w", err)
		}
	} else if !i.projector.Empty() {
		// Handle object

		// Apply the projector to reduce the attributes included in each item down to only those of interest to the
		// caller.
		item, err := i.adapter.EvaluateProjector(i.projector, objectResult)
		if err != nil {
			// Not likely a 500 error so send a 400 and return nil instead
			return i.adapter.Error(i.original, err.Error(), http.StatusBadRequest)
		}

		i.original.WriteHeader(i.statusCode)
		err = json.NewEncoder(i.original).Encode(item)
		if err != nil {
			return fmt.Errorf("failed to encode object: %w", err)
		}
	} else {
		// No projector, and selectors don't apply to Get requests.
		i.original.WriteHeader(i.statusCode)
		_, err = i.original.Write(i.buf.Bytes())
		if err != nil {
			return fmt.Errorf("failed to write to response: %w", err)
		}
		return nil
	}

	return nil
}

// ResponseFilter intercepts the response body and removes fields that are not required.
func ResponseFilter(adapter *FilterAdapter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				slog.Error("failed to parse query", "RawQuery", r.URL.RawQuery, "err", err)
				_ = adapter.Error(
					w,
					fmt.Sprintf("failed to parse query: %s; error: %s", r.URL.RawQuery, err.Error()),
					http.StatusBadRequest,
				)
				return
			}

			var projector search.Projector
			var selector search.Selector
			if values, ok := query[excludeFields]; ok && len(values) > 0 {
				projector.Exclude, err = adapter.ParseFields(values...)
				if err != nil {
					slog.Error("failed to parse exclude field exclude_fields", "path", values, "err", err)
					_ = adapter.Error(w, fmt.Sprintf("failed to parse exclude fields: %s", values), http.StatusBadRequest)
					return
				}
			}

			if values, ok := query[fields]; ok && len(values) > 0 {
				projector.Include, err = adapter.ParseFields(values...)
				if err != nil {
					slog.Error("failed to parse field fields", "path", values, "err", err)
					_ = adapter.Error(w, fmt.Sprintf("failed to parse include fields: %s", values), http.StatusBadRequest)
					return
				}
			}

			if !projector.Empty() {
				// Get the required fields for this request and make sure they are always included and never excluded
				if err = adapter.EnforceRequiredFields(&projector, r); err != nil {
					slog.Warn("unable to determine required fields for request", "request", r.URL, "error", err)
					// For now, we'll treat this as a warning to handle any unexpected case
				}
			}

			if values, ok := query[filter]; ok && len(values) > 0 {
				for _, value := range values {
					result, err := adapter.ParseFilter(value)
					if err != nil {
						slog.Error("failed to parse filter", "value", value, "err", err)
						_ = adapter.Error(w, fmt.Sprintf("failed to parse filter: %s", value), http.StatusBadRequest)
						return
					}
					selector.Terms = append(selector.Terms, result.Terms...)
				}
			}

			// Override the response writer with an FilterResponseInterceptor so we can capture the output
			interceptor := &FilterResponseInterceptor{
				original:  w,
				adapter:   adapter,
				projector: &projector,
				selector:  &selector,
			}

			next.ServeHTTP(interceptor, r)

			if err = interceptor.Flush(r); err != nil {
				text := fmt.Sprintf("failed to flush interceptor: %s", err.Error())
				_ = adapter.Error(w, text, http.StatusInternalServerError)
			}
		})
	}
}

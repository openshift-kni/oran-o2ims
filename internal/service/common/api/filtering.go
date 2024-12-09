package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

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
	pathsParser        *search.PathsParser
	projectorEvaluator *search.ProjectorEvaluator
	selectorEvaluator  *search.SelectorEvaluator
	selectorParser     *search.SelectorParser
}

// NewFilterAdapter creates a new filter adapter to be passed to a ResponseFilter
func NewFilterAdapter(logger *slog.Logger) (*FilterAdapter, error) {
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

	return &FilterAdapter{
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

// ParseFilter delegates the function of parsing the filter fields to the selector parser.
func (a *FilterAdapter) ParseFilter(query string) (*search.Selector, error) {
	selector, err := a.selectorParser.Parse(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse filter: %w", err)
	}
	return selector, nil
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
	projector  search.Projector
	selector   search.Selector
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
func (i *FilterResponseInterceptor) Flush() error {
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
				ok, err := i.adapter.EvaluateSelector(&i.selector, item)
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
			// caller.
			for index, item := range items {
				mappedItem, err := i.adapter.EvaluateProjector(&i.projector, item)
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
		item, err := i.adapter.EvaluateProjector(&i.projector, objectResult)
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
			query := r.URL.Query()
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
				projector: projector,
				selector:  selector,
			}

			next.ServeHTTP(interceptor, r)

			if err = interceptor.Flush(); err != nil {
				text := fmt.Sprintf("failed to flush interceptor: %s", err.Error())
				_ = adapter.Error(w, text, http.StatusInternalServerError)
			}
		})
	}
}

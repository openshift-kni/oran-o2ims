package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"

	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

type Middleware = func(http.Handler) http.Handler

// ChainHandlers applies each middleware in order to the base router.
func ChainHandlers(base http.Handler, wrappers ...Middleware) http.Handler {
	h := base
	for _, wrap := range wrappers {
		h = wrap(h)
	}
	return h
}

// LogDuration log time taken to complete a request.
// TODO: This is just get started with middleware but should be replaced with something that's more suitable for production i.e OpenTelemetry
// https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/examples/prometheus/main.go
func LogDuration() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()
			next.ServeHTTP(w, r)
			slog.Debug(fmt.Sprintf("%s took %s", r.RequestURI, time.Since(startTime)))
		})
	}
}

// OpenAPIValidation to findFieldByName all incoming requests as specified in the spec
func OpenAPIValidation(swagger *openapi3.T) Middleware {
	// Clear out the servers array in the swagger spec, that skips validating
	// that server names match. We don't know how this thing will be run.
	swagger.Servers = nil

	// explicitly register `merge-patch+json` needed for validation during patch requests
	openapi3filter.RegisterBodyDecoder("application/merge-patch+json", openapi3filter.JSONBodyDecoder)

	return oapimiddleware.OapiRequestValidatorWithOptions(swagger, &oapimiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, // No auth needed even when we have something in spec
		},
		ErrorHandler: getOranErrHandler(),
	})
}

// TrailingSlashStripper allow API calls with trailing "/"
func TrailingSlashStripper() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// problemDetails writes an error message using the appropriate header for an ORAN error response
func problemDetails(w http.ResponseWriter, body string, code int) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(code)
	_, err := fmt.Fprintln(w, body)
	if err != nil {
		panic(err)
	}
}

// getOranErrHandler override default validation error to allow for O-RAN specific error
func getOranErrHandler() func(w http.ResponseWriter, message string, statusCode int) {
	return func(w http.ResponseWriter, message string, statusCode int) {
		out, _ := json.Marshal(common.ProblemDetails{
			Detail: message,
			Status: statusCode,
		})
		problemDetails(w, string(out), statusCode)
	}
}

// GetOranReqErrFunc override default validation errors to allow for O-RAN specific struct
func GetOranReqErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		out, _ := json.Marshal(common.ProblemDetails{
			Detail: err.Error(),
			Status: http.StatusBadRequest,
		})
		problemDetails(w, string(out), http.StatusBadRequest)
	}
}

// GetOranRespErrFunc override default internal server error to allow for O-RAN specific struct
func GetOranRespErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		out, _ := json.Marshal(common.ProblemDetails{
			Detail: err.Error(),
			Status: http.StatusInternalServerError,
		})
		problemDetails(w, string(out), http.StatusInternalServerError)
	}
}

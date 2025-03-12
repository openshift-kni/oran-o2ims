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
	"github.com/google/uuid"
	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/openshift-kni/oran-hwmgr-plugin/pkg/inventory-client/generated"
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

type durationLogger struct {
	http.ResponseWriter
	statusCode int
}

func (d *durationLogger) WriteHeader(statusCode int) {
	d.statusCode = statusCode
	d.ResponseWriter.WriteHeader(statusCode)
}

// LogDuration log time taken to complete a request.
// TODO: This is just get started with middleware but should be replaced with something that's more suitable for production i.e OpenTelemetry
// https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/examples/prometheus/main.go
func LogDuration() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()
			d := durationLogger{
				ResponseWriter: w,
			}
			next.ServeHTTP(&d, r)
			slog.Debug("Request completed", "method", r.Method, "url", r.RequestURI, "status", d.statusCode, "duration", time.Since(startTime).String())
		})
	}
}

// UUIDValidator ensures a valid UUID in request bodies
type UUIDValidator struct{}

// Validate checks if a string is a valid UUID
func (v UUIDValidator) Validate(value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return err // nolint: wrapcheck
	}
	return nil
}

// OpenAPIValidation to findFieldByName all incoming requests as specified in the spec
func OpenAPIValidation(swagger *openapi3.T) Middleware {
	// Clear out the servers array in the swagger spec, that skips validating
	// that server names match. We don't know how this thing will be run.
	swagger.Servers = nil

	// explicitly register `merge-patch+json` needed for validation during patch requests
	openapi3filter.RegisterBodyDecoder("application/merge-patch+json", openapi3filter.JSONBodyDecoder)

	// explicitly enable validation for uuid format
	openapi3.DefineStringFormatValidator("uuid", UUIDValidator{})

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

// ProblemDetails writes an error message using the appropriate header for an ORAN error response
func ProblemDetails(w http.ResponseWriter, body string, code int) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(code)
	out, _ := json.Marshal(generated.ProblemDetails{
		Detail: body,
		Status: code,
	})
	_, err := fmt.Fprintln(w, string(out))
	if err != nil {
		panic(err)
	}
}

// getOranErrHandler override default validation error to allow for O-RAN specific error
func getOranErrHandler() func(w http.ResponseWriter, message string, statusCode int) {
	return func(w http.ResponseWriter, message string, statusCode int) {
		ProblemDetails(w, message, statusCode)
	}
}

// GetOranReqErrFunc override default validation errors to allow for O-RAN specific struct
func GetOranReqErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		ProblemDetails(w, err.Error(), http.StatusBadRequest)
	}
}

// GetOranRespErrFunc override default internal server error to allow for O-RAN specific struct
func GetOranRespErrFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		ProblemDetails(w, err.Error(), http.StatusInternalServerError)
	}
}

package internal

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

type Middleware = func(http.Handler) http.Handler

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

// AlarmsOapiValidation to validate all incoming requests as specified in the spec
func AlarmsOapiValidation() Middleware {
	// This also validates the spec file
	swagger, err := api.GetSwagger()
	if err != nil {
		// Panic will allow for defer statements to execute
		panic(fmt.Sprintf("failed to get swagger: %s", err))
	}

	// Clear out the servers array in the swagger spec, that skips validating
	// that server names match. We don't know how this thing will be run.
	swagger.Servers = nil

	return oapimiddleware.OapiRequestValidatorWithOptions(swagger, &oapimiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, // No auth needed even when we have something in spec
		},
		ErrorHandler: getOranErrHandler(),
	})
}

// getOranErrHandler override default validation error to allow for O-RAN specific error
func getOranErrHandler() func(w http.ResponseWriter, message string, statusCode int) {
	return func(w http.ResponseWriter, message string, statusCode int) {
		out, _ := json.Marshal(common.ProblemDetails{
			Detail: message,
			Status: statusCode,
		})
		http.Error(w, string(out), statusCode)
	}
}

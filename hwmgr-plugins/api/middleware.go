/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/generated/server"
)

type Middleware = func(http.Handler) http.Handler

type durationLogger struct {
	http.ResponseWriter
	statusCode int
}

func (d *durationLogger) WriteHeader(statusCode int) {
	d.statusCode = statusCode
	d.ResponseWriter.WriteHeader(statusCode)
}

// GetLogDurationFunc log time taken to complete a request.
func GetLogDurationFunc() Middleware {
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

// GetOpenAPIValidationFunc to validate all incoming requests as specified in the spec
func GetOpenAPIValidationFunc(swagger *openapi3.T) Middleware {
	// Clear out the servers array in the swagger spec, that skips validating
	// that server names match. We don't know how this thing will be run.
	swagger.Servers = nil

	return oapimiddleware.OapiRequestValidatorWithOptions(swagger, &oapimiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, // No auth needed even when we have something in spec
		},
		ErrorHandler: getErrorHandlerFunc(),
	})
}

// ProblemDetails writes an error message using the appropriate header for an error response
func ProblemDetails(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(code)
	body, _ := json.Marshal(server.ProblemDetails{
		Detail: message,
		Status: code,
	})
	_, err := fmt.Fprintln(w, string(body))
	if err != nil {
		panic(err)
	}
}

// getErrorHandlerFunc override default validation error to allow for specific error
func getErrorHandlerFunc() func(w http.ResponseWriter, message string, statusCode int) {
	return func(w http.ResponseWriter, message string, statusCode int) {
		ProblemDetails(w, message, statusCode)
	}
}

// GracefulShutdown allow graceful shutdown with timeout
func GracefulShutdown(srv *http.Server) error {
	// Create shutdown context with 10 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed graceful shutdown: %w", err)
	}

	slog.Info("Server gracefully stopped")
	return nil
}

// GetRequestErrorFunc override default validation errors to allow for specific struct
func GetRequestErrorFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		ProblemDetails(w, err.Error(), http.StatusBadRequest)
	}
}

// GetResponseErrorFunc override default internal server error to allow for specific struct
func GetResponseErrorFunc() func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		ProblemDetails(w, err.Error(), http.StatusInternalServerError)
	}
}

// GetNotFoundFunc is used to override the default 404 response which is a text only reply so that we can respond with
// the required JSON body.
func GetNotFoundFunc() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ProblemDetails(w, fmt.Sprintf("Path '%s' not found", r.RequestURI), http.StatusNotFound)
	}
}

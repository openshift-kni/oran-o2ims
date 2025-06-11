/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

// interceptor defines an implementation of a workaround for a limitation of the http.ServeMux.
// http.ServeMux does not allow customizing the error handlers to write JSON formatted responses
// instead of plain text.  To meet our interface requirements, we need to respond with JSON
// formatted error responses in all cases.
//
// see: https://github.com/golang/go/issues/65648
type interceptor struct {
	original    http.ResponseWriter
	statusCode  int
	intercepted bool
}

// Header returns the headers stored in the underlying original ResponseWriter
func (e *interceptor) Header() http.Header {
	return e.original.Header()
}

// WriteHeader sets the status code and determines if a plain text header has already been set.  If
// so, then the header is overwritten to an application/problem+json header with the expectation
// that when the Write method is called with the actual error text that it will be reformatted to
// the expected JSON response.
func (e *interceptor) WriteHeader(statusCode int) {
	if strings.Contains(e.original.Header().Get("Content-Type"), "text/plain") {
		e.original.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		e.intercepted = true
	}
	e.statusCode = statusCode
	e.original.WriteHeader(statusCode)
}

// Write determines whether the data to be written should be passed through directly the underlying
// buffer or if it needs to be converted to JSON first based on what that of header was written
// previously using the WriteHeader method.
func (e *interceptor) Write(data []byte) (int, error) {
	var out []byte
	if e.intercepted {
		out, _ = json.Marshal(common.ProblemDetails{
			Detail: strings.TrimSpace(string(data)),
			Status: e.statusCode,
		})
	} else {
		out = data
	}
	return e.original.Write(out) //nolint:wrapcheck
}

// ErrorJsonifier return oran json structure instead of the default plain text
func ErrorJsonifier() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&interceptor{original: w}, r)
		})
	}
}

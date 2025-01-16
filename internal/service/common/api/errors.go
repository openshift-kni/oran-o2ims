package api

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

// ErrorJsonifier wraps a http.ServeMux so that we can override plain text error responses to JSON
type ErrorJsonifier struct {
	mux *http.ServeMux
}

// NewErrorJsonifier creates a new instance of an ErrorJsonifier
func NewErrorJsonifier(router *http.ServeMux) *ErrorJsonifier {
	return &ErrorJsonifier{mux: router}
}

// HandleFunc calls the HandleFunc method on the original http.ServeMux
func (e *ErrorJsonifier) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	e.mux.HandleFunc(pattern, handler)
}

// ServeHTTP calls the ServeHTTP method on the original http.ServeMux by substituting the
// ResponseWriter with an implementation that can intercept plain text error responses and convert
// them to JSON.
func (e *ErrorJsonifier) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	e.mux.ServeHTTP(&interceptor{
		original: writer,
	}, request)
}

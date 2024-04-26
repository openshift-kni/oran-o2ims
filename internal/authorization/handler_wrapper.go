/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package authorization

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"

	"github.com/openshift-kni/oran-o2ims/internal/authentication"

	jsoniter "github.com/json-iterator/go"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// HandlerWrapperBuilder contains the data and logic needed to create a wrapper that knows how to
// convert an HTTP handler into another one that also performs authorization using the claims of
// the authenticated subject.
//
// Don't create instances of this object directly, use the NewHandlerWrapper function instead.
type HandlerWrapperBuilder struct {
	logger      *slog.Logger
	publicPaths []string
	aclFiles    []string
}

type handlerWrapper struct {
	logger      *slog.Logger
	publicPaths []*regexp.Regexp
	aclItems    map[string]*regexp.Regexp
	jsonAPI     jsoniter.API
}

type handlerObject struct {
	wrapper *handlerWrapper
	handler http.Handler
}

// NewHandlerWrapper creates a builder that can then be configured and used to create authorization
// handler wrappers. This wrapper is a function that transforms an HTTP handler into another that
// performs authorization using the claims of the authenticated subject.
func NewHandlerWrapper() *HandlerWrapperBuilder {
	return &HandlerWrapperBuilder{}
}

// SetLogger sets the logger that the handlers will use to send messages to the log. This is
// mandatory.
func (b *HandlerWrapperBuilder) SetLogger(value *slog.Logger) *HandlerWrapperBuilder {
	b.logger = value
	return b
}

// AddPublicPath adds a regular expression that defines parts of the URL space that considered
// public, and therefore require no authorization. This method may be called multiple times and
// then all the given regular expressions will be used to check what parts of the URL space are
// public.
func (b *HandlerWrapperBuilder) AddPublicPath(value string) *HandlerWrapperBuilder {
	b.publicPaths = append(b.publicPaths, value)
	return b
}

// SetFlags sets the command line flags that should be used to configure the wrapper. This is
// optional.
func (b *HandlerWrapperBuilder) SetFlags(flags *pflag.FlagSet) *HandlerWrapperBuilder {
	if flags != nil {
		if flags.Changed(aclFileFlagName) {
			values, err := flags.GetStringArray(aclFileFlagName)
			if err == nil {
				for _, value := range values {
					b.AddACLFile(value)
				}
			}
		}
	}
	return b
}

// AddACLFile adds a file that contains items of the access control list. This should be a YAML file
// with the following format:
//
//   - claim: email
//     pattern: ^.*@redhat\.com$
//
//   - claim: sub
//     pattern: ^f:b3f7b485-7184-43c8-8169-37bd6d1fe4aa:myuser$
//
// The claim field is the name of the claim of the subject that will be checked. The pattern field
// is a regular expression. If the claim matches the regular expression then access will be allowed.
//
// If the ACL is empty then access will be allowed to all subjects.
//
// If the ACL has at least one item then access will be allowed only to subjects that match at least
// one of the items.
func (b *HandlerWrapperBuilder) AddACLFile(value string) *HandlerWrapperBuilder {
	if value != "" {
		b.aclFiles = append(b.aclFiles, value)
	}
	return b
}

// Build uses the data stored in the builder to create a new authentication handler.
func (b *HandlerWrapperBuilder) Build() (result func(http.Handler) http.Handler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}

	// Try to compile the regular expressions that define the parts of the URL space that are
	// publicPaths:
	publicPaths := make([]*regexp.Regexp, len(b.publicPaths))
	for i, expr := range b.publicPaths {
		publicPaths[i], err = regexp.Compile(expr)
		if err != nil {
			return
		}
	}

	// Load the ACL files:
	aclItems := map[string]*regexp.Regexp{}
	for _, file := range b.aclFiles {
		err = b.loadACLFile(file, aclItems)
		if err != nil {
			return
		}
	}

	// Create the JSON API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create and populate the object:
	wrapper := &handlerWrapper{
		logger:      b.logger,
		publicPaths: publicPaths,
		aclItems:    aclItems,
		jsonAPI:     jsonAPI,
	}
	result = wrapper.wrap

	return
}

// aclItem is the type used to read a single ACL item from a YAML document.
type aclItem struct {
	Claim   string `yaml:"claim"`
	Pattern string `yaml:"pattern"`
}

// loadACLFile loads the given ACL file into the given map of ACL items.
func (b *HandlerWrapperBuilder) loadACLFile(file string, items map[string]*regexp.Regexp) error {
	// Load the YAML data:
	yamlData, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	// Parse the YAML data:
	var listData []aclItem
	err = yaml.Unmarshal(yamlData, &listData)
	if err != nil {
		return err
	}

	// Process the items:
	for _, itemData := range listData {
		items[itemData.Claim], err = regexp.Compile(itemData.Pattern)
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *handlerWrapper) wrap(handler http.Handler) http.Handler {
	return &handlerObject{
		wrapper: h,
		handler: handler,
	}
}

func (h *handlerWrapper) serve(handler http.Handler, w http.ResponseWriter, r *http.Request) {
	// Get the context:
	ctx := r.Context()

	// Check if the requested path is public, and skip authorization if it is:
	for _, expr := range h.publicPaths {
		if expr.MatchString(r.URL.Path) {
			handler.ServeHTTP(w, r)
			return
		}
	}

	// Get the subject and check the ACL and send an error response if there is no match:
	subject := authentication.SubjectFromContext(ctx)
	if !h.checkACL(subject.Claims) {
		h.logger.InfoContext(
			ctx,
			"Access denied",
			slog.String("subject", subject.Name),
			slog.Any("claims", subject.Claims),
			slog.String("path", r.URL.Path),
		)
		h.sendError(w, r)
		return
	}

	// There was a match, so call the wrapped handler:
	handler.ServeHTTP(w, r)
}

// checkACL checks if the given set of claims match at least one of the items of the access control
// list.
func (h *handlerWrapper) checkACL(claims map[string]any) bool {
	// If there are no ACL items we consider that there are no restrictions, therefore we
	// return true immediately:
	if len(h.aclItems) == 0 {
		return true
	}

	// Check all the ACL items:
	for claim, pattern := range h.aclItems {
		value, ok := claims[claim]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if pattern.MatchString(text) {
			return true
		}
	}

	// No match, so the access is denied:
	return false
}

// sendError sends an error response to the client with the message of the given error.
func (h *handlerWrapper) sendError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusForbidden)
	writer := jsoniter.NewStream(h.jsonAPI, w, 512)
	writer.WriteObjectStart()
	writer.WriteObjectField("status")
	writer.WriteInt(http.StatusForbidden)
	writer.WriteMore()
	writer.WriteObjectField("detail")
	writer.WriteString("Access denied")
	writer.WriteObjectEnd()
	writer.Flush()
}

// ServeHTTP is the implementation of the http.Handler interface.
func (h *handlerObject) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.wrapper.serve(h.handler, w, r)
}

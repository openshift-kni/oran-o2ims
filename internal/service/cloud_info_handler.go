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

package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/data"
)

// CloudInfoHandlerBuilder contains the data and logic needed to create a new handler for the application
// root. Don't create instances of this type directly, use the NewCloudInfoHandler function instead.
type CloudInfoHandlerBuilder struct {
	logger  *slog.Logger
	cloudID string
}

// RootHander knows how to respond to requests for the application root. Don't create instances of
// this type directly, use the NewCloudInfoHandler function instead.
type CloudInfoHandler struct {
	logger  *slog.Logger
	cloudID string
}

// NewCloudInfoHandler creates a builder that can then be used to configure and create a handler for the
// root of the application.
func NewCloudInfoHandler() *CloudInfoHandlerBuilder {
	return &CloudInfoHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *CloudInfoHandlerBuilder) SetLogger(value *slog.Logger) *CloudInfoHandlerBuilder {
	b.logger = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *CloudInfoHandlerBuilder) SetCloudID(value string) *CloudInfoHandlerBuilder {
	b.cloudID = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *CloudInfoHandlerBuilder) Build() (result *CloudInfoHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.cloudID == "" {
		err = errors.New("cloud identifier is mandatory")
		return
	}

	// Create and populate the object:
	result = &CloudInfoHandler{
		logger:  b.logger,
		cloudID: b.cloudID,
	}
	return
}

// Get is part of the implementation of the object handler interface.
func (h *CloudInfoHandler) Get(ctx context.Context, request *ObjectRequest) (response *ObjectResponse,
	err error) {
	response = &ObjectResponse{
		Object: data.Object{
			"oCloudId":      h.cloudID,
			"globalCloudId": h.cloudID,
			"name":          "OpenShift O-Cloud",
			"description":   "OpenShift O-Cloud",
			"serviceUri":    "https://localhost:8080",
			"extensions":    data.Object{},
		},
	}
	return
}

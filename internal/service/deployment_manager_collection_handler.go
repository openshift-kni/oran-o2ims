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

	"github.com/jhernand/o2ims/internal/data"
	"github.com/jhernand/o2ims/internal/streaming"
)

// DeploymentManagerCollectionHandlerBuilder contains the data and logic needed to create a new
// deployment manager collection handler. Don't create instances of this type directly, use the
// NewDeploymentManagerCollectionHandler function instead.
type DeploymentManagerCollectionHandlerBuilder struct {
	logger  *slog.Logger
	cloudID string
}

// DeploymentManagerCollectionHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewDeploymentManagerCollectionHandler
// function instead.
type DeploymentManagerCollectionHandler struct {
	logger  *slog.Logger
	cloudID string
}

// NewDeploymentManagerCollectionHandler creates a builder that can then be used to configure
// and create a handler for the collection of deployment managers.
func NewDeploymentManagerCollectionHandler() *DeploymentManagerCollectionHandlerBuilder {
	return &DeploymentManagerCollectionHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *DeploymentManagerCollectionHandlerBuilder) SetLogger(
	value *slog.Logger) *DeploymentManagerCollectionHandlerBuilder {
	b.logger = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *DeploymentManagerCollectionHandlerBuilder) SetCloudID(
	value string) *DeploymentManagerCollectionHandlerBuilder {
	b.cloudID = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *DeploymentManagerCollectionHandlerBuilder) Build() (
	result *DeploymentManagerCollectionHandler, err error) {
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
	result = &DeploymentManagerCollectionHandler{
		logger:  b.logger,
		cloudID: b.cloudID,
	}
	return
}

// Get is part of the implementation of the collection handler interface.
func (h *DeploymentManagerCollectionHandler) Get(ctx context.Context,
	request *CollectionRequest) (response *CollectionResponse, err error) {
	count := 10
	response = &CollectionResponse{
		Items: data.StreamFunc(func(ctx context.Context) (item data.Object, err error) {
			if count == 0 {
				err = streaming.ErrEnd
				return
			}
			item = data.Object{
				{
					Name:  "deploymentManagerId",
					Value: "123",
				},
				{
					Name:  "name",
					Value: "dm-123",
				},
				{
					Name:  "description",
					Value: "Deployment manager 123",
				},
				{
					Name:  "oCloudId",
					Value: h.cloudID,
				},
				{
					Name:  "serviceUri",
					Value: "https://...",
				},
				{
					Name:  "capabilities",
					Value: data.Object{},
				},
				{
					Name: "extensions",
					Value: data.Object{
						{
							Name:  "a",
							Value: "A",
						},
						{
							Name:  "b",
							Value: "B",
						},
					},
				},
			}
			count--
			return
		}),
	}
	return
}

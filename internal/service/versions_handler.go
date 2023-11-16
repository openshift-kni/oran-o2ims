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
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/coreos/go-semver/semver"

	"github.com/openshift-kni/oran-o2ims/internal/data"
)

// VersionsHandlerBuilder contains the data and logic needed to create a new handler that servers
// the list of versions of the API. Don't create instances of this type directly, use the
// NewVersionsHandler function instead.
type VersionsHandlerBuilder struct {
	logger *slog.Logger
}

// RootHander knows how to respond to requests for the the list of versions of the API. Don't
// create instances of this type directly, use the NewVersionsHandler function instead.
type VersionsHandler struct {
	logger *slog.Logger
}

// NewVersionsHandler creates a builder that can then be used to configure and create a handler for the
// list of versions of the API.
func NewVersionsHandler() *VersionsHandlerBuilder {
	return &VersionsHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *VersionsHandlerBuilder) SetLogger(value *slog.Logger) *VersionsHandlerBuilder {
	b.logger = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *VersionsHandlerBuilder) Build() (result *VersionsHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create and populate the object:
	result = &VersionsHandler{
		logger: b.logger,
	}
	return
}

// Get is part of the implementation of the object handler interface.
func (h *VersionsHandler) Get(ctx context.Context, request *ObjectRequest) (response *ObjectResponse,
	err error) {
	// If a specifc major version was included in the URL then we need to select and return
	// only the ones that match that:
	var selectedVersions []data.Object
	if request.ID != "" {
		selectedVersions = make([]data.Object, 0, 1)
		if !strings.HasPrefix(request.ID, "v") {
			err = fmt.Errorf(
				"version identifier '%s' isn't valid, it should start with 'v'",
				request.ID,
			)
			return
		}
		var majorNumber int
		majorNumber, err = strconv.Atoi(request.ID[1:])
		if err != nil {
			return
		}
		for _, currentVersion := range allVersions {
			versionValue, ok := currentVersion["version"]
			if !ok {
				h.logger.Error(
					"Version doesn't have a version number, will ignore it",
					slog.Any("version", currentVersion),
				)
				continue
			}
			versionText, ok := versionValue.(string)
			if !ok {
				h.logger.Error(
					"Version number isn't a string, will ignore it",
					slog.Any("version", versionValue),
				)
				continue
			}
			versionNumber, err := semver.NewVersion(versionText)
			if err != nil {
				h.logger.Error(
					"Version number isn't a valid semantic version, will ignore it",
					slog.String("version", versionText),
					slog.String("error", err.Error()),
				)
				continue
			}
			if versionNumber.Major == int64(majorNumber) {
				selectedVersions = append(selectedVersions, currentVersion)
			}
		}
	} else {
		selectedVersions = allVersions
	}

	// Return the result:
	response = &ObjectResponse{
		Object: data.Object{
			"uriPrefix":   "/o2ims-infrastructureInventory/v1",
			"apiVersions": selectedVersions,
		},
	}
	return
}

var allVersions = []data.Object{
	{
		"version": "1.0.0",
	},
}

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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/files"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
)

// AlarmProbableCauseHandlerBuilder contains the data and logic needed to create a new alarm
// probable cause collection handler. Don't create instances of this type directly, use the NewAlarmProbableCauseHandler
// function instead.
type AlarmProbableCauseHandlerBuilder struct {
	logger *slog.Logger
}

// AlarmProbableCauseHandler knows how to respond to requests to list alarms. Don't create
// instances of this type directly, use the NewAlarmProbableCauseHandler function instead.
type AlarmProbableCauseHandler struct {
	logger *slog.Logger
}

// NewAlarmProbableCauseHandler creates a builder that can then be used to configure and create a
// handler for the collection of alarms.
func NewAlarmProbableCauseHandler() *AlarmProbableCauseHandlerBuilder {
	return &AlarmProbableCauseHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *AlarmProbableCauseHandlerBuilder) SetLogger(
	value *slog.Logger) *AlarmProbableCauseHandlerBuilder {
	b.logger = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *AlarmProbableCauseHandlerBuilder) Build() (
	result *AlarmProbableCauseHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create and populate the object:
	result = &AlarmProbableCauseHandler{
		logger: b.logger,
	}
	return
}

// List is part of the implementation of the collection handler interface.
func (h *AlarmProbableCauseHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {

	// Transform the items into what we need:
	definitions, err := h.fetchItems()
	if err != nil {
		return
	}

	// Return the result:
	response = &ListResponse{
		Items: definitions,
	}
	return
}

// Get is part of the implementation of the object handler interface.
func (h *AlarmProbableCauseHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {

	// Fetch the object:
	definition, err := h.fetchItem(ctx, request.Variables[0])
	if err != nil {
		return
	}

	// Return the result:
	response = &GetResponse{
		Object: definition,
	}

	return
}

func (h *AlarmProbableCauseHandler) fetchItems() (result data.Stream, err error) {
	jsonFile, err := files.Alarms.ReadFile(alarmsProbableCausesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file at path %s: %w", alarmsProbableCausesPath, err)
	}
	reader := bytes.NewReader(jsonFile)

	definitions, err := k8s.NewStream().
		SetLogger(h.logger).
		SetReader(reader).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build stream from reader: %w", err)
	}

	// Transform AlarmProbableCauses
	result = data.Map(definitions, h.mapItem)

	return
}

func (h *AlarmProbableCauseHandler) fetchItem(ctx context.Context,
	id string) (probableCause data.Object, err error) {

	probableCauses, err := h.fetchItems()
	if err != nil {
		return
	}

	// Filter by ID
	probableCauses = data.Select(
		probableCauses,
		func(ctx context.Context, item data.Object) (result bool, err error) {
			result = item["probableCauseId"] == id
			return
		},
	)

	// Get first result
	probableCause, err = probableCauses.Next(ctx)

	return
}

// Map Definition to an O2 AlarmProbableCause object.
func (h *AlarmProbableCauseHandler) mapItem(ctx context.Context,
	from data.Object) (to data.Object, err error) {

	probableCauseId, err := data.GetString(from, "probableCauseId")
	if err != nil {
		return
	}

	name, err := data.GetString(from, "name")
	if err != nil {
		return
	}

	description, err := data.GetString(from, "description")
	if err != nil {
		return
	}

	to = data.Object{
		"probableCauseId": probableCauseId,
		"name":            name,
		"description":     description,
	}

	return
}

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

	jsoniter "github.com/json-iterator/go"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/files"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
)

const (
	alarmsDefinitionsPath    = "alarms/definitions.json"
	alarmsProbableCausesPath = "alarms/probable_causes.json"
)

// AlarmDefinitionHandlerBuilder contains the data and logic needed to create a new alarm
// definition collection handler. Don't create instances of this type directly, use the NewAlarmDefinitionHandler
// function instead.
type AlarmDefinitionHandlerBuilder struct {
	logger *slog.Logger
}

// AlarmDefinitionHandler knows how to respond to requests to list alarms. Don't create
// instances of this type directly, use the NewAlarmDefinitionHandler function instead.
type AlarmDefinitionHandler struct {
	logger  *slog.Logger
	jsonAPI jsoniter.API
}

// NewAlarmDefinitionHandler creates a builder that can then be used to configure and create a
// handler for the collection of alarms.
func NewAlarmDefinitionHandler() *AlarmDefinitionHandlerBuilder {
	return &AlarmDefinitionHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *AlarmDefinitionHandlerBuilder) SetLogger(
	value *slog.Logger) *AlarmDefinitionHandlerBuilder {
	b.logger = value
	return b
}

// Build uses the data stored in the builder to create and configure a new handler.
func (b *AlarmDefinitionHandlerBuilder) Build() (
	result *AlarmDefinitionHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Prepare the JSON iterator API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create and populate the object:
	result = &AlarmDefinitionHandler{
		logger:  b.logger,
		jsonAPI: jsonAPI,
	}
	return
}

// List is part of the implementation of the collection handler interface.
func (h *AlarmDefinitionHandler) List(ctx context.Context,
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
func (h *AlarmDefinitionHandler) Get(ctx context.Context,
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

func (h *AlarmDefinitionHandler) fetchItems() (result data.Stream, err error) {
	jsonFile, err := files.Alarms.ReadFile(alarmsDefinitionsPath)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(jsonFile)

	definitions, err := k8s.NewStream().
		SetLogger(h.logger).
		SetReader(reader).
		Build()

	// Transform to AlarmDefinitions objects
	result = data.Map(definitions, h.mapItem)

	return
}

func (h *AlarmDefinitionHandler) fetchItem(ctx context.Context,
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

// Map Definition to an O2 AlarmDefinitions object.
func (h *AlarmDefinitionHandler) mapItem(ctx context.Context,
	from data.Object) (to data.Object, err error) {

	alarmDefinitionId, err := data.GetString(from, "alarmDefinitionId")
	if err != nil {
		return
	}

	alarmName, err := data.GetString(from, "alarmName")
	if err != nil {
		return
	}

	alarmDescription, err := data.GetString(from, "alarmDescription")
	if err != nil {
		return
	}

	proposedRepairActions, err := data.GetString(from, "proposedRepairActions")
	if err != nil {
		// Property is optional
		h.logger.Debug(fmt.Sprintf("'%s' is missing from alarm definition (optional)", "proposedRepairActions"))
	}

	alarmAdditionalFields, err := data.GetObj(from, "alarmAdditionalFields")
	if err != nil {
		// Property is optional
		h.logger.Debug(fmt.Sprintf("'%s' is missing from alarm definition (optional)", "alarmAdditionalFields"))
		err = nil
	}

	to = data.Object{
		"alarmDefinitionId":     alarmDefinitionId,
		"alarmName":             alarmName,
		"alarmDescription":      alarmDescription,
		"proposedRepairActions": proposedRepairActions,
		"managementInterfaceId": "O2IMS",
		"pkNotificationField":   "alarmDefinitionID",
		"alarmAdditionalFields": alarmAdditionalFields,
	}

	return
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ResourceInfo describes a hardware resource (BMH) for inventory purposes.
type ResourceInfo struct {
	AdminState       ResourceInfoAdminState       `json:"adminState"`
	Allocated        *bool                        `json:"allocated,omitempty"`
	Description      string                       `json:"description"`
	GlobalAssetId    *string                      `json:"globalAssetId,omitempty"`
	Groups           *[]string                    `json:"groups,omitempty"`
	HwProfile        string                       `json:"hwProfile"`
	Labels           *map[string]string           `json:"labels,omitempty"`
	Memory           int                          `json:"memory"`
	Model            string                       `json:"model"`
	Name             string                       `json:"name"`
	Nics             *map[string]NicInfo          `json:"nics,omitempty"`
	OperationalState ResourceInfoOperationalState `json:"operationalState"`
	PowerState       *ResourceInfoPowerState      `json:"powerState,omitempty"`
	Processors       []ProcessorInfo              `json:"processors"`
	ResourceId       openapi_types.UUID           `json:"resourceId"`
	ResourcePoolId   openapi_types.UUID           `json:"resourcePoolId"`
	Storage          *map[string]StorageInfo      `json:"storage,omitempty"`
	Tags             *[]string                    `json:"tags,omitempty"`
	UsageState       ResourceInfoUsageState       `json:"usageState"`
	Vendor           string                       `json:"vendor"`
}

type ResourceInfoAdminState string

const (
	ResourceInfoAdminStateLOCKED       ResourceInfoAdminState = "LOCKED"
	ResourceInfoAdminStateSHUTTINGDOWN ResourceInfoAdminState = "SHUTTING_DOWN"
	ResourceInfoAdminStateUNKNOWN      ResourceInfoAdminState = "UNKNOWN"
	ResourceInfoAdminStateUNLOCKED     ResourceInfoAdminState = "UNLOCKED"
)

type ResourceInfoOperationalState string

const (
	ResourceInfoOperationalStateDISABLED ResourceInfoOperationalState = "DISABLED"
	ResourceInfoOperationalStateENABLED  ResourceInfoOperationalState = "ENABLED"
	ResourceInfoOperationalStateUNKNOWN  ResourceInfoOperationalState = "UNKNOWN"
)

type ResourceInfoPowerState string

const (
	OFF ResourceInfoPowerState = "OFF"
	ON  ResourceInfoPowerState = "ON"
)

type ResourceInfoUsageState string

const (
	ACTIVE  ResourceInfoUsageState = "ACTIVE"
	BUSY    ResourceInfoUsageState = "BUSY"
	IDLE    ResourceInfoUsageState = "IDLE"
	UNKNOWN ResourceInfoUsageState = "UNKNOWN"
)

// NicInfo describes a network interface card.
type NicInfo struct {
	BootInterface *bool   `json:"bootInterface,omitempty"`
	Label         *string `json:"label,omitempty"`
	Mac           *string `json:"mac,omitempty"`
	Model         *string `json:"model,omitempty"`
	SpeedGbps     *int    `json:"speedGbps,omitempty"`
}

// ProcessorInfo describes a processor.
type ProcessorInfo struct {
	Architecture *string `json:"architecture,omitempty"`
	Cpus         *int    `json:"cpus,omitempty"`
	Frequency    *int    `json:"frequency,omitempty"`
	Model        *string `json:"model,omitempty"`
}

// StorageInfo describes a storage device.
type StorageInfo struct {
	AlternateNames *[]string        `json:"alternateNames,omitempty"`
	Model          *string          `json:"model,omitempty"`
	SerialNumber   *string          `json:"serialNumber,omitempty"`
	SizeBytes      *int64           `json:"sizeBytes,omitempty"`
	Type           *StorageInfoType `json:"type,omitempty"`
	Wwn            *string          `json:"wwn,omitempty"`
}

type StorageInfoType string

const (
	HDD  StorageInfoType = "HDD"
	NVME StorageInfoType = "NVME"
	SSD  StorageInfoType = "SSD"
)

// ProblemDetails is the standard RFC 7807 error response format.
type ProblemDetails struct {
	AdditionalAttributes *map[string]string `json:"additionalAttributes,omitempty"`
	Detail               string             `json:"detail"`
	Instance             *string            `json:"instance,omitempty"`
	Status               int                `json:"status"`
	Title                *string            `json:"title,omitempty"`
	Type                 *string            `json:"type,omitempty"`
}

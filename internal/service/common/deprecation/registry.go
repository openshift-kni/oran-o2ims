/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

// Package deprecation provides utilities for tracking and signaling deprecated API fields
// following RFC 8594 (Sunset Header) best practices.
package deprecation

import (
	"sort"
	"strings"
	"time"
)

const resourcesAPIBasePath = "/o2ims-infrastructureInventory/v1" //nolint:unused // Reserved for future deprecations

// Field represents metadata about a deprecated API field
type Field struct {
	// EndpointPath is the URL path prefix where this deprecated field is returned
	// (e.g., "/o2ims-infrastructureInventory/v1/resourcePools")
	EndpointPath string

	// Schema is the name of the schema containing the deprecated field (e.g., "ResourcePool")
	Schema string

	// FieldName is the name of the deprecated field (e.g., "oCloudId")
	FieldName string

	// SunsetDate is when the field will be removed (per RFC 8594)
	SunsetDate time.Time

	// Replacement describes what to use instead (empty if no replacement)
	Replacement string

	// Reason explains why the field is being deprecated
	Reason string

	// MigrationGuide is the relative path to the migration documentation
	MigrationGuide string
}

// registry holds all deprecated fields. Single source of truth.
// To add new deprecations, create a Field entry here and create a migration guide in /docs/deprecations/
var registry = []Field{
	/*
		This is only an example:
		{
		 	EndpointPath:   resourcesAPIBasePath + "/resourceTypes",
		 	Schema:         "ResourceType",
		 	FieldName:      "alarmDictionary",
		 	SunsetDate:     sunset2026Dec01,
		 	Replacement:    "alarmDictionaryId",
		 	Reason:         "Full embedded object replaced with ID reference for efficiency",
		 	MigrationGuide: "/docs/deprecations/resource-type-fields.md",
		}
	*/
}

// endpointFieldsCache maps URL path prefixes to deprecated fields.
var endpointFieldsCache map[string][]Field

func init() {
	endpointFieldsCache = buildEndpointFieldsCache()
}

// buildEndpointFieldsCache groups registry fields by their EndpointPath and sorts by SunsetDate.
// - Key: Full URL path prefix from Field.EndpointPath
// - Value: All Field structs sharing that endpoint path, sorted by SunsetDate (earliest first)
func buildEndpointFieldsCache() map[string][]Field {
	cache := make(map[string][]Field)

	for _, field := range registry {
		cache[field.EndpointPath] = append(cache[field.EndpointPath], field)
	}

	// Sort by SunsetDate
	for _, fields := range cache {
		sort.Slice(fields, func(i, j int) bool {
			return fields[i].SunsetDate.Before(fields[j].SunsetDate)
		})
	}

	return cache
}

// GetFieldsForEndpoint returns deprecated fields relevant to an endpoint path.
func GetFieldsForEndpoint(path string) []Field {
	for pathPrefix, fields := range endpointFieldsCache {
		if strings.HasPrefix(path, pathPrefix) {
			return fields
		}
	}
	return nil
}

// GetSunsetDate returns the sunset date for the provided fields.
// The first one is returned
func GetSunsetDate(fields []Field) time.Time {
	if len(fields) == 0 {
		return time.Time{}
	}
	return fields[0].SunsetDate
}

// GetMigrationGuide returns the migration guide path for the first deprecated field.
// If multiple fields have different guides, the first one is returned.
func GetMigrationGuide(fields []Field) string {
	for _, field := range fields {
		if field.MigrationGuide != "" {
			return field.MigrationGuide
		}
	}
	return ""
}

// HasDeprecatedFields checks if the given endpoint returns any deprecated fields.
func HasDeprecatedFields(path string) bool {
	return len(GetFieldsForEndpoint(path)) > 0
}

// DaysUntilSunset returns the number of days until the sunset date.
// Returns negative if the sunset date has passed.
func DaysUntilSunset(sunsetDate time.Time) int {
	return int(time.Until(sunsetDate).Hours() / 24)
}

// SetRegistry replaces the registry with custom fields and rebuilds the cache.
func SetRegistry(fields []Field) {
	registry = fields
	endpointFieldsCache = buildEndpointFieldsCache()
}

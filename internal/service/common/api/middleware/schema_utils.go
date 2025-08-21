/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package middleware

import (
	"log/slog"

	"github.com/getkin/kin-openapi/openapi3"
)

// NewFilterAdapterFromSwagger creates a FilterAdapter with schemas extracted from an OpenAPI specification.
// This is a convenience function that extracts all component schemas from the swagger spec and passes
// them to NewFilterAdapterWithSchemas for field validation.
func NewFilterAdapterFromSwagger(logger *slog.Logger, swagger *openapi3.T) (*FilterAdapter, error) {
	schemas := extractSchemasFromSwagger(swagger)
	return NewFilterAdapterWithSchemas(logger, schemas)
}

// extractSchemasFromSwagger extracts all component schemas from an OpenAPI specification.
// Returns a map of schema names to schema objects that can be used for field validation.
func extractSchemasFromSwagger(swagger *openapi3.T) map[string]*openapi3.Schema {
	schemas := make(map[string]*openapi3.Schema)

	if swagger != nil && swagger.Components != nil && swagger.Components.Schemas != nil {
		for name, schemaRef := range swagger.Components.Schemas {
			if schemaRef != nil && schemaRef.Value != nil {
				schemas[name] = schemaRef.Value
			}
		}
	}

	return schemas
}

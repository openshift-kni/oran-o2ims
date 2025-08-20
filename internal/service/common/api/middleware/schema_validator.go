/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package middleware

import (
	"github.com/getkin/kin-openapi/openapi3"
)

// SchemaValidator validates field names against OpenAPI schemas
type SchemaValidator struct {
	schemas map[string]*openapi3.Schema
}

// NewSchemaValidator creates a new schema validator with the provided schemas
func NewSchemaValidator(schemas map[string]*openapi3.Schema) *SchemaValidator {
	return &SchemaValidator{
		schemas: schemas,
	}
}

// ValidateFieldPath validates that a field path exists in any of the provided schemas
// Returns true if the field is valid (exists in at least one schema), false otherwise
func (v *SchemaValidator) ValidateFieldPath(fieldPath []string) bool {
	if len(fieldPath) == 0 {
		return false
	}

	// Check each schema to see if any contains this field path
	for _, schema := range v.schemas {
		if v.validateFieldInSchema(fieldPath, schema) {
			return true
		}
	}
	return false
}

// validateFieldInSchema recursively validates a field path against a specific schema
func (v *SchemaValidator) validateFieldInSchema(fieldPath []string, schema *openapi3.Schema) bool {
	if len(fieldPath) == 0 {
		return true
	}

	if schema == nil {
		return false
	}

	// Handle object schemas
	if (schema.Type != nil && schema.Type.Is("object")) || len(schema.Properties) > 0 {
		fieldName := fieldPath[0]

		// Check if the field exists in this schema's properties
		if property, exists := schema.Properties[fieldName]; exists {
			// If this is the last field in the path, it's valid
			if len(fieldPath) == 1 {
				return true
			}
			// Continue validation with the remaining path
			return v.validateFieldInSchema(fieldPath[1:], property.Value)
		}

		// Check if the schema has additionalProperties that allow any field
		if schema.AdditionalProperties.Has != nil && *schema.AdditionalProperties.Has {
			return true
		}

		// Check allOf, anyOf, oneOf schemas
		for _, subSchema := range schema.AllOf {
			if v.validateFieldInSchema(fieldPath, subSchema.Value) {
				return true
			}
		}
		for _, subSchema := range schema.AnyOf {
			if v.validateFieldInSchema(fieldPath, subSchema.Value) {
				return true
			}
		}
		for _, subSchema := range schema.OneOf {
			if v.validateFieldInSchema(fieldPath, subSchema.Value) {
				return true
			}
		}
	}

	// Handle array schemas
	if schema.Type != nil && schema.Type.Is("array") && schema.Items != nil {
		// For arrays, validate against the item schema
		return v.validateFieldInSchema(fieldPath, schema.Items.Value)
	}

	return false
}

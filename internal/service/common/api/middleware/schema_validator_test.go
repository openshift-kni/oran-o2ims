/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package middleware

import (
	"github.com/getkin/kin-openapi/openapi3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SchemaValidator", func() {
	var validator *SchemaValidator

	BeforeEach(func() {
		// Create test schemas
		schemas := map[string]*openapi3.Schema{
			"Resource": {
				Type: &openapi3.Types{"object"},
				Properties: map[string]*openapi3.SchemaRef{
					"id": {
						Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
					},
					"name": {
						Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
					},
					"metadata": {
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
							Properties: map[string]*openapi3.SchemaRef{
								"created": {
									Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
								},
								"labels": {
									Value: &openapi3.Schema{Type: &openapi3.Types{"object"}},
								},
							},
						},
					},
					"optionalField": {
						Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
					},
				},
			},
		}
		validator = NewSchemaValidator(schemas)
	})

	It("should validate existing top-level fields", func() {
		Expect(validator.ValidateFieldPath([]string{"id"})).To(BeTrue())
		Expect(validator.ValidateFieldPath([]string{"name"})).To(BeTrue())
		Expect(validator.ValidateFieldPath([]string{"optionalField"})).To(BeTrue())
	})

	It("should validate nested fields", func() {
		Expect(validator.ValidateFieldPath([]string{"metadata", "created"})).To(BeTrue())
		Expect(validator.ValidateFieldPath([]string{"metadata", "labels"})).To(BeTrue())
	})

	It("should reject invalid field names", func() {
		Expect(validator.ValidateFieldPath([]string{"nonexistent"})).To(BeFalse())
		Expect(validator.ValidateFieldPath([]string{"metadata", "nonexistent"})).To(BeFalse())
	})

	It("should handle empty field paths", func() {
		Expect(validator.ValidateFieldPath([]string{})).To(BeFalse())
	})

	It("should handle schemas with additionalProperties", func() {
		schemas := map[string]*openapi3.Schema{
			"FlexibleResource": {
				Type: &openapi3.Types{"object"},
				Properties: map[string]*openapi3.SchemaRef{
					"id": {
						Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
					},
				},
				AdditionalProperties: openapi3.AdditionalProperties{
					Has: func() *bool { b := true; return &b }(),
				},
			},
		}
		flexValidator := NewSchemaValidator(schemas)

		// Should accept any field name when additionalProperties is true
		Expect(flexValidator.ValidateFieldPath([]string{"id"})).To(BeTrue())
		Expect(flexValidator.ValidateFieldPath([]string{"anyField"})).To(BeTrue())
	})
})

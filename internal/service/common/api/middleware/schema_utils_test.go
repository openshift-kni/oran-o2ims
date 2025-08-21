/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package middleware

import (
	"log/slog"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SchemaUtils", func() {
	var logger *slog.Logger

	BeforeEach(func() {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	})

	Describe("extractSchemasFromSwagger", func() {
		It("should extract schemas from a valid swagger spec", func() {
			swagger := &openapi3.T{
				Components: &openapi3.Components{
					Schemas: map[string]*openapi3.SchemaRef{
						"User": {
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"object"},
								Properties: map[string]*openapi3.SchemaRef{
									"id": {
										Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
									},
									"name": {
										Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
									},
								},
							},
						},
						"Product": {
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"object"},
								Properties: map[string]*openapi3.SchemaRef{
									"id": {
										Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
									},
									"title": {
										Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
									},
								},
							},
						},
					},
				},
			}

			schemas := extractSchemasFromSwagger(swagger)

			Expect(schemas).To(HaveLen(2))
			Expect(schemas).To(HaveKey("User"))
			Expect(schemas).To(HaveKey("Product"))
			Expect(schemas["User"].Type.Is("object")).To(BeTrue())
			Expect(schemas["Product"].Type.Is("object")).To(BeTrue())
		})

		It("should return empty map for nil swagger", func() {
			schemas := extractSchemasFromSwagger(nil)
			Expect(schemas).To(BeEmpty())
		})

		It("should return empty map for swagger without components", func() {
			swagger := &openapi3.T{}
			schemas := extractSchemasFromSwagger(swagger)
			Expect(schemas).To(BeEmpty())
		})

		It("should handle nil schema refs gracefully", func() {
			swagger := &openapi3.T{
				Components: &openapi3.Components{
					Schemas: map[string]*openapi3.SchemaRef{
						"ValidSchema": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"object"}},
						},
						"NilRef": nil,
						"EmptyRef": {
							Value: nil,
						},
					},
				},
			}

			schemas := extractSchemasFromSwagger(swagger)

			Expect(schemas).To(HaveLen(1))
			Expect(schemas).To(HaveKey("ValidSchema"))
			Expect(schemas).NotTo(HaveKey("NilRef"))
			Expect(schemas).NotTo(HaveKey("EmptyRef"))
		})
	})

	Describe("NewFilterAdapterFromSwagger", func() {
		It("should create a filter adapter with schema validation", func() {
			swagger := &openapi3.T{
				Components: &openapi3.Components{
					Schemas: map[string]*openapi3.SchemaRef{
						"TestSchema": {
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"object"},
								Properties: map[string]*openapi3.SchemaRef{
									"validField": {
										Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
									},
								},
							},
						},
					},
				},
			}

			adapter, err := NewFilterAdapterFromSwagger(logger, swagger)

			Expect(err).NotTo(HaveOccurred())
			Expect(adapter).NotTo(BeNil())
			Expect(adapter.schemaValidator).NotTo(BeNil())
		})

		It("should work with nil swagger (no schema validation)", func() {
			adapter, err := NewFilterAdapterFromSwagger(logger, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(adapter).NotTo(BeNil())
			Expect(adapter.schemaValidator).To(BeNil())
		})
	})
})

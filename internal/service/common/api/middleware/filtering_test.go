/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testFilterParams = "filter=(eq,name,hello)%3b(eq,value,1)"

var _ = Describe("ResponseFilter", func() {
	var (
		recorder *httptest.ResponseRecorder
		req      *http.Request
		handler  http.Handler
		adapter  *FilterAdapter
		next     http.HandlerFunc
	)

	type object struct {
		Name          string  `json:"name"`
		Value         int     `json:"value"`
		OptionalField *string `json:"optionalField,omitempty"`
	}

	BeforeEach(func() {
		recorder = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/some-endpoint", nil)
		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		var err error
		adapter, err = NewFilterAdapter(logger)
		Expect(err).NotTo(HaveOccurred())

		list := []object{{Name: "hello", Value: 1}, {Name: "world", Value: 10}}
		body, err := json.Marshal(list)
		Expect(err).NotTo(HaveOccurred())

		next = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		}
		handler = ResponseFilter(adapter)(next)
	})

	It("should process the list request and respond correctly", func() {
		req.URL.RawQuery = testFilterParams
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusOK))
		var list []object
		err := json.Unmarshal(recorder.Body.Bytes(), &list)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))
		Expect(list[0].Name).To(Equal("hello"))
		Expect(list[0].Value).To(Equal(1))
	})

	It("should not process the filter if an error is returned", func() {
		list := []object{{Name: "hello", Value: 1}, {Name: "world", Value: 10}}
		body, err := json.Marshal(list)
		Expect(err).NotTo(HaveOccurred())
		next = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(body)
		}
		handler = ResponseFilter(adapter)(next)

		req.URL.RawQuery = testFilterParams
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		err = json.Unmarshal(recorder.Body.Bytes(), &list)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(2))
	})

	It("should not process the filter on a get request", func() {
		record := object{Name: "hello", Value: 1}
		body, err := json.Marshal(record)
		Expect(err).NotTo(HaveOccurred())
		next = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		}
		handler = ResponseFilter(adapter)(next)

		req.URL.RawQuery = testFilterParams
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusOK))
		err = json.Unmarshal(recorder.Body.Bytes(), &record)
		Expect(err).NotTo(HaveOccurred())
		Expect(record.Name).To(Equal("hello"))
	})

	It("should process the list request without a filter", func() {
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusOK))
		var list []object
		err := json.Unmarshal(recorder.Body.Bytes(), &list)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(2))
	})

	It("should return an empty list if the filtering excludes all list items", func() {
		req.URL.RawQuery = "filter=(eq,name,foo)%3b(eq,value,10)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusOK))
		var list []object
		err := json.Unmarshal(recorder.Body.Bytes(), &list)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(0))
	})

	It("should return bad request on invalid query", func() {
		// Bad ';' ... needs to be encoded
		req.URL.RawQuery = "filter=(eq,name,foo);(eq,value,10)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	})

	It("should return bad request on invalid filter", func() {
		req.URL.RawQuery = "filter=(xxx,name,foo)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	})

	It("should handle unknown field name gracefully when no schema validation", func() {
		req.URL.RawQuery = "filter=(eq,unknown,foo)"
		handler.ServeHTTP(recorder, req)

		// Without schema validation, unknown fields should be handled gracefully
		// and return an empty result since no objects have that field
		Expect(recorder.Code).To(Equal(http.StatusOK))
		var result []object
		err := json.Unmarshal(recorder.Body.Bytes(), &result)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(0))
	})

	It("should return bad request on type mismatch", func() {
		req.URL.RawQuery = "filter=(eq,value,foo)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	})

	It("should return internal server error on flush failure", func() {
		next = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`invalid json`))
		}
		handler = ResponseFilter(adapter)(next)

		req.URL.RawQuery = "filter=(eq,name,foo)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
	})

	It("should handle filtering on optional fields gracefully", func() {
		// Create a list with objects where some have the optional field and some don't
		valueWithOptional := "optional_value"
		list := []object{
			{Name: "hello", Value: 1, OptionalField: &valueWithOptional},
			{Name: "world", Value: 10, OptionalField: nil},
		}
		body, err := json.Marshal(list)
		Expect(err).NotTo(HaveOccurred())

		next = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		}
		handler = ResponseFilter(adapter)(next)

		// Filter by an optional field that only exists in some objects
		req.URL.RawQuery = "filter=(eq,optionalField,optional_value)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusOK))
		var result []object
		err = json.Unmarshal(recorder.Body.Bytes(), &result)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Name).To(Equal("hello"))
		Expect(result[0].OptionalField).NotTo(BeNil())
		Expect(*result[0].OptionalField).To(Equal("optional_value"))
	})

	It("should handle filtering on missing optional fields without errors", func() {
		// Create a list where none of the objects have the optional field populated
		list := []object{
			{Name: "hello", Value: 1, OptionalField: nil},
			{Name: "world", Value: 10, OptionalField: nil},
		}
		body, err := json.Marshal(list)
		Expect(err).NotTo(HaveOccurred())

		next = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		}
		handler = ResponseFilter(adapter)(next)

		// Filter by an optional field that doesn't exist in any object
		req.URL.RawQuery = "filter=(eq,optionalField,some_value)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusOK))
		var result []object
		err = json.Unmarshal(recorder.Body.Bytes(), &result)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(0)) // No objects should match since the field is nil
	})

	Context("with schema validation", func() {
		var schemaAdapter *FilterAdapter

		BeforeEach(func() {
			// Create test schemas that match our object structure
			schemas := map[string]*openapi3.Schema{
				"TestObject": {
					Type: &openapi3.Types{"object"},
					Properties: map[string]*openapi3.SchemaRef{
						"name": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
						"value": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
						},
						"optionalField": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
			}

			logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
			var err error
			schemaAdapter, err = NewFilterAdapterWithSchemas(logger, schemas)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return bad request for truly invalid field names with schema validation", func() {
			list := []object{{Name: "hello", Value: 1}, {Name: "world", Value: 10}}
			body, err := json.Marshal(list)
			Expect(err).NotTo(HaveOccurred())

			next := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			}
			handler := ResponseFilter(schemaAdapter)(http.HandlerFunc(next))

			req.URL.RawQuery = "filter=(eq,invalidField,foo)"
			handler.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))

			// Verify the error message is user-friendly
			var errorResponse map[string]interface{}
			err = json.Unmarshal(recorder.Body.Bytes(), &errorResponse)
			Expect(err).NotTo(HaveOccurred())
			Expect(errorResponse["detail"]).To(ContainSubstring("invalid field name 'invalidField'"))
			Expect(errorResponse["detail"]).To(ContainSubstring("does not exist in the API schema"))
		})

		It("should return descriptive error for typos in field names", func() {
			list := []object{{Name: "hello", Value: 1}, {Name: "world", Value: 10}}
			body, err := json.Marshal(list)
			Expect(err).NotTo(HaveOccurred())

			next := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			}
			handler := ResponseFilter(schemaAdapter)(http.HandlerFunc(next))

			// Test with a typo in field name (filte2r instead of filter)
			req.URL.RawQuery = "filter=(eq,filte2r,ACK)"
			handler.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))

			// Verify the error message specifically mentions the typo
			var errorResponse map[string]interface{}
			err = json.Unmarshal(recorder.Body.Bytes(), &errorResponse)
			Expect(err).NotTo(HaveOccurred())
			Expect(errorResponse["detail"]).To(ContainSubstring("invalid field name 'filte2r'"))
			Expect(errorResponse["detail"]).To(ContainSubstring("does not exist in the API schema"))
		})

		It("should allow valid optional fields with schema validation", func() {
			valueWithOptional := "optional_value"
			list := []object{
				{Name: "hello", Value: 1, OptionalField: &valueWithOptional},
				{Name: "world", Value: 10, OptionalField: nil},
			}
			body, err := json.Marshal(list)
			Expect(err).NotTo(HaveOccurred())

			next := func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			}
			handler := ResponseFilter(schemaAdapter)(http.HandlerFunc(next))

			req.URL.RawQuery = "filter=(eq,optionalField,optional_value)"
			handler.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			var result []object
			err = json.Unmarshal(recorder.Body.Bytes(), &result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("hello"))
		})
	})
})

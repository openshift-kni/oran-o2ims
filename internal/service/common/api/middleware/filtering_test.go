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
		Name  string `json:"name"`
		Value int    `json:"value"`
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

	It("should return bad request on unknown field name", func() {
		req.URL.RawQuery = "filter=(eq,unknown,foo)"
		handler.ServeHTTP(recorder, req)

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
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
})

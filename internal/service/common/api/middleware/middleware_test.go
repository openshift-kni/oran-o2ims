/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"

	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validation error logging", func() {
	var (
		buf      bytes.Buffer
		original *slog.Logger
	)

	BeforeEach(func() {
		buf.Reset()
		original = slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	})

	AfterEach(func() {
		slog.SetDefault(original)
	})

	Describe("getOranErrHandler", func() {
		It("should log a warning with structured fields on validation failure", func() {
			handler := getOranErrHandler()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/o2ims-infrastructureInventory/v1/resourcePools", nil)

			handler(context.Background(), errors.New("request body has an error"), recorder, req, oapimiddleware.ErrorHandlerOpts{
				StatusCode: http.StatusBadRequest,
			})

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))

			var logEntry map[string]any
			Expect(json.Unmarshal(buf.Bytes(), &logEntry)).To(Succeed())
			Expect(logEntry).To(HaveKeyWithValue("level", "WARN"))
			Expect(logEntry).To(HaveKeyWithValue("msg", "OpenAPI validation failed"))
			Expect(logEntry).To(HaveKeyWithValue("error", "request body has an error"))
			Expect(logEntry).To(HaveKeyWithValue("method", "POST"))
			Expect(logEntry).To(HaveKeyWithValue("path", "/o2ims-infrastructureInventory/v1/resourcePools"))
			Expect(logEntry).To(HaveKeyWithValue("status", BeNumerically("==", http.StatusBadRequest)))
		})
	})

	Describe("GetOranReqErrFunc", func() {
		It("should log a warning with structured fields on request validation failure", func() {
			handler := GetOranReqErrFunc()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/o2ims-infrastructureInventory/v1/deploymentManagers", nil)

			handler(recorder, req, errors.New("parameter 'filter' has invalid value"))

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))

			var logEntry map[string]any
			Expect(json.Unmarshal(buf.Bytes(), &logEntry)).To(Succeed())
			Expect(logEntry).To(HaveKeyWithValue("level", "WARN"))
			Expect(logEntry).To(HaveKeyWithValue("msg", "OpenAPI request validation failed"))
			Expect(logEntry).To(HaveKeyWithValue("error", "parameter 'filter' has invalid value"))
			Expect(logEntry).To(HaveKeyWithValue("method", "GET"))
			Expect(logEntry).To(HaveKeyWithValue("path", "/o2ims-infrastructureInventory/v1/deploymentManagers"))
			Expect(logEntry).To(HaveKeyWithValue("status", BeNumerically("==", http.StatusBadRequest)))
		})
	})
})

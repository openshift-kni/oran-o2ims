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
	"fmt"
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
			req := httptest.NewRequest(http.MethodPost, "/o2ims-infrastructureInventory/v2/resourcePools", nil)

			handler(context.Background(), errors.New("request body has an error"), recorder, req, oapimiddleware.ErrorHandlerOpts{
				StatusCode: http.StatusBadRequest,
			})

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))

			var logEntry map[string]any
			Expect(json.Unmarshal(buf.Bytes(), &logEntry)).To(Succeed())
			Expect(logEntry).To(HaveKeyWithValue("level", "WARN"))
			Expect(logEntry).To(HaveKeyWithValue("msg", "OpenAPI validation failed"))
			Expect(logEntry).To(HaveKeyWithValue("error", "request body has an error"))
			Expect(logEntry).To(HaveKeyWithValue("status", BeNumerically("==", http.StatusBadRequest)))
		})
	})

	Describe("GetOranReqErrFunc", func() {
		It("should log a warning with structured fields on request validation failure", func() {
			handler := GetOranReqErrFunc()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/o2ims-infrastructureInventory/v2/deploymentManagers", nil)

			handler(recorder, req, errors.New("parameter 'filter' has invalid value"))

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))

			var logEntry map[string]any
			Expect(json.Unmarshal(buf.Bytes(), &logEntry)).To(Succeed())
			Expect(logEntry).To(HaveKeyWithValue("level", "WARN"))
			Expect(logEntry).To(HaveKeyWithValue("msg", "OpenAPI request validation failed"))
			Expect(logEntry).To(HaveKeyWithValue("error", "parameter 'filter' has invalid value"))
			Expect(logEntry).To(HaveKeyWithValue("status", BeNumerically("==", http.StatusBadRequest)))
		})

		It("should strip raw input from InvalidParamFormatError messages in the response", func() {
			handler := GetOranReqErrFunc()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/o2ims-infrastructureInventory/v2/subscriptions/bad-value", nil)

			maliciousInput := "<script>alert(1)</script>"
			//nolint:revive // Capitalized to match oapi-codegen's InvalidParamFormatError.Error() output
			err := fmt.Errorf("Invalid format for parameter subscriptionId: error unmarshaling '%s' text as *uuid.UUID: invalid UUID length: 25", maliciousInput)
			handler(recorder, req, err)

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			body := recorder.Body.String()
			Expect(body).ToNot(ContainSubstring(maliciousInput))
			Expect(body).To(ContainSubstring("Invalid format for parameter subscriptionId"))
			Expect(body).ToNot(ContainSubstring("error unmarshaling"))

			var logEntry map[string]any
			Expect(json.Unmarshal(buf.Bytes(), &logEntry)).To(Succeed())
			Expect(logEntry).To(HaveKeyWithValue("error", ContainSubstring(maliciousInput)))
		})

		It("should pass through non-InvalidParamFormatError messages unchanged", func() {
			handler := GetOranReqErrFunc()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/o2ims-infrastructureInventory/v2/subscriptions", nil)

			handler(recorder, req, errors.New("request body has an error: value is required"))

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("request body has an error: value is required"))
		})
	})
})

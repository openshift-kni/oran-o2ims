/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Send error", func() {
	It("Sends the 'application/problem+json' content type", func() {
		recorder := httptest.NewRecorder()
		SendError(recorder, http.StatusBadRequest, "")
		Expect(recorder.Header().Get("Content-Type")).To(Equal("application/problem+json"))
	})

	DescribeTable(
		"Sends the status code in the header and the body",
		func(code int) {
			recorder := httptest.NewRecorder()
			SendError(recorder, code, "")
			Expect(recorder.Code).To(Equal(code))
			var body struct {
				Status int `json:"status"`
			}
			err := json.Unmarshal(recorder.Body.Bytes(), &body)
			Expect(err).ToNot(HaveOccurred())
			Expect(body.Status).To(Equal(code))
		},
		Entry("Bad request", http.StatusBadRequest),
		Entry("Internal server error", http.StatusInternalServerError),
		Entry("Not found", http.StatusNotFound),
	)

	DescribeTable(
		"Formats the details",
		func(expected string, msg string, args ...any) {
			recorder := httptest.NewRecorder()
			SendError(recorder, http.StatusBadRequest, msg, args...)
			var body struct {
				Detail string `json:"detail"`
			}
			err := json.Unmarshal(recorder.Body.Bytes(), &body)
			Expect(err).ToNot(HaveOccurred())
			Expect(body.Detail).To(Equal(expected))
		},
		Entry(
			"No format verbs",
			"Something failed",
			"Something failed",
		),
		Entry(
			"Format verbs",
			"File 'myfile.txt' doesn't exist and 32 is an even number",
			"File '%s' doesn't exist and %d is an even number", "myfile.txt", 32,
		),
	)
})

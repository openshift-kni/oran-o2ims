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

package openapi

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/decorators"
	. "github.com/onsi/gomega"

	. "github.com/openshift-kni/oran-o2ims/internal/testing"
)

var _ = Describe("Handler", func() {
	It("Returns a valid JSON content type and document", func() {
		// Create the handler:
		handler, err := NewHandler().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(handler).ToNot(BeNil())

		// Send the request:
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(recorder, request)

		// Verify the content type:
		Expect(recorder.Code).To(Equal(http.StatusOK))
		contentType := recorder.Header().Get("Content-Type")
		Expect(contentType).ToNot(BeEmpty())
		mediaType, _, err := mime.ParseMediaType(contentType)
		Expect(err).ToNot(HaveOccurred())
		Expect(mediaType).To(Equal("application/json"))

		// Verify the content format:
		var spec any
		err = json.Unmarshal(recorder.Body.Bytes(), &spec)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Content", Ordered, func() {
		var spec any

		BeforeAll(func() {
			// Create the handler:
			handler, err := NewHandler().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(handler).ToNot(BeNil())

			// Send the request:
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			handler.ServeHTTP(recorder, request)

			// Parse the response:
			Expect(recorder.Code).To(Equal(http.StatusOK))
			err = json.Unmarshal(recorder.Body.Bytes(), &spec)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Contains the basic fields", func() {
			Expect(spec).To(MatchJQ(`.openapi`, "3.0.0"))
			Expect(spec).To(MatchJQ(`.info.title`, "O2 IMS"))
			Expect(spec).To(MatchJQ(`.info.version`, "1.0.0"))
		})

		It("Contains at least one path", func() {
			Expect(spec).To(MatchJQ(`(.paths | length) > 0`, true))
		})

		It("Contains at least one schema", func() {
			Expect(spec).To(MatchJQ(`(.components.schemas | length) > 0`, true))
		})

		It("All paths start with the expected prefix", func() {
			Expect(spec).To(MatchJQ(`[.paths | keys[] | select(startswith("/o2ims-infrastructureInventory/") | not)] | length`, 0))
		})

		It("Contains the expected schemas", func() {
			Expect(spec).To(MatchJQ(`any(.components.schemas | keys[]; . == "APIVersion")`, true))
			Expect(spec).To(MatchJQ(`any(.components.schemas | keys[]; . == "APIVersions")`, true))
			Expect(spec).To(MatchJQ(`any(.components.schemas | keys[]; . == "DeploymentManager")`, true))
		})
	})
})

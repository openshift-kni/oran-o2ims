/*
Copyright 2024 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

// This file contains tests for the metrics transport wrapper.

package metrics

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/ghttp"

	. "github.com/openshift-kni/oran-o2ims/internal/testing"
)

var _ = Describe("Create", func() {
	It("Can't be created without a subsystem", func() {
		wrapper, err := NewHandlerWrapper().
			Build()
		Expect(err).To(HaveOccurred())
		Expect(wrapper).To(BeNil())
		message := err.Error()
		Expect(message).To(ContainSubstring("subsystem"))
		Expect(message).To(ContainSubstring("mandatory"))
	})
})

var _ = Describe("Metrics", func() {
	var (
		server  *MetricsServer
		wrapper func(http.Handler) http.Handler
		handler http.Handler
	)

	BeforeEach(func() {
		var err error

		// Start the metrics server:
		server = NewMetricsServer()

		// Create the wrapper:
		wrapper, err = NewHandlerWrapper().
			AddPaths(
				"/my",
				"/my/v1/resources",
				"/my/v1/resources/-",
				"/my/v1/resources/-/action",
				"/my/v1/resources/-/groups",
				"/my/v1/resources/-/groups",
				"/my/v1/resources/-/groups/-",
			).
			SetSubsystem("my").
			SetRegisterer(server.Registry()).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Stop the metrics server:
		server.Close()
	})

	// Get sends a GET request to the API server.
	var Send = func(method, path string) {
		request := httptest.NewRequest(method, "http://localhost"+path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
	}

	It("Calls wrapped handler", func() {
		// Prepare the handler:
		called := false
		handler = wrapper(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))

		// Send the request:
		Send(http.MethodGet, "/")

		// Check that the wrapped handler was called:
		Expect(called).To(BeTrue())
	})

	Describe("Request count", func() {
		It("Honours subsystem", func() {
			// Prepare the handler:
			handler = wrapper(RespondWith(http.StatusOK, nil))

			// Send the request:
			Send(http.MethodGet, "/")

			// Verify the metrics:
			metrics := server.Metrics()
			Expect(metrics).To(MatchLine(`^my_request_count\{.*\} .*$`))
		})

		DescribeTable(
			"Counts correctly",
			func(count int) {
				// Prepare the handler:
				handler = wrapper(RespondWith(http.StatusOK, nil))

				// Send the requests:
				for i := 0; i < count; i++ {
					Send(http.MethodGet, "/")
				}

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_count\{.*\} %d$`, count))
			},
			Entry("One", 1),
			Entry("Two", 2),
			Entry("Trhee", 3),
		)

		DescribeTable(
			"Includes method label",
			func(method string) {
				// Prepare the handler:
				handler = wrapper(RespondWith(http.StatusOK, nil))

				// Send the requests:
				Send(method, "/")

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_count\{.*method="%s".*\} .*$`, method))
			},
			Entry("GET", http.MethodGet),
			Entry("POST", http.MethodPost),
			Entry("PATCH", http.MethodPatch),
			Entry("DELETE", http.MethodDelete),
		)

		DescribeTable(
			"Includes path label",
			func(path, label string) {
				// Prepare the handler:
				handler = wrapper(RespondWith(http.StatusOK, nil))

				// Send the requests:
				Send(http.MethodGet, path)

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_count\{.*path="%s".*\} .*$`, label))
			},
			Entry(
				"Empty",
				"",
				"/",
			),
			Entry(
				"One slash",
				"/",
				"/",
			),
			Entry(
				"Two slashes",
				"//",
				"/",
			),
			Entry(
				"Tree slashes",
				"///",
				"/",
			),
			Entry(
				"Unknown root",
				"/junk/",
				"/-",
			),
			Entry(
				"Service root",
				"/my",
				"/my",
			),
			Entry(
				"Unknown service root",
				"/junk",
				"/-",
			),
			Entry(
				"Version root",
				"/my/v1",
				"/my/v1",
			),
			Entry(
				"Unknown version root",
				"/junk/v1",
				"/-",
			),
			Entry(
				"Collection",
				"/my/v1/resources",
				"/my/v1/resources",
			),
			Entry(
				"Unknown collection",
				"/my/v1/junk",
				"/-",
			),
			Entry(
				"Collection item",
				"/my/v1/resources/123",
				"/my/v1/resources/-",
			),
			Entry(
				"Collection item action",
				"/my/v1/resources/123/action",
				"/my/v1/resources/-/action",
			),
			Entry(
				"Unknown collection item action",
				"/my/v1/resources/123/junk",
				"/-",
			),
			Entry(
				"Subcollection",
				"/my/v1/resources/123/groups",
				"/my/v1/resources/-/groups",
			),
			Entry(
				"Unknown subcollection",
				"/my/v1/resources/123/junks",
				"/-",
			),
			Entry(
				"Subcollection item",
				"/my/v1/resources/123/groups/456",
				"/my/v1/resources/-/groups/-",
			),
			Entry(
				"Too long",
				"/my/v1/resources/123/groups/456/junk",
				"/-",
			),
			Entry(
				"Unknown path",
				"/your/path",
				"/-",
			),
		)

		DescribeTable(
			"Includes code label",
			func(code int) {
				// Prepare the handler:
				handler = wrapper(RespondWith(code, nil))

				// Send the requests:
				Send(http.MethodGet, "/")

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_count\{.*code="%d".*\} .*$`, code))
			},
			Entry("200", http.StatusOK),
			Entry("201", http.StatusCreated),
			Entry("202", http.StatusAccepted),
			Entry("401", http.StatusUnauthorized),
			Entry("404", http.StatusNotFound),
			Entry("500", http.StatusInternalServerError),
		)
	})

	Describe("Request duration", func() {
		It("Honours subsystem", func() {
			// Prepare the handler:
			handler = wrapper(RespondWith(http.StatusOK, nil))

			// Send the request:
			Send(http.MethodGet, "/")

			// Verify the metrics:
			metrics := server.Metrics()
			Expect(metrics).To(MatchLine(`^my_request_duration_bucket\{.*\} .*$`))
			Expect(metrics).To(MatchLine(`^my_request_duration_sum\{.*\} .*$`))
			Expect(metrics).To(MatchLine(`^my_request_duration_count\{.*\} .*$`))
		})

		It("Honours buckets", func() {
			// Prepare the handler:
			handler = wrapper(RespondWith(http.StatusOK, nil))

			// Send the request:
			Send(http.MethodGet, "/")

			// Verify the metrics:
			metrics := server.Metrics()
			Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*,le="0.1"\} .*$`))
			Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*,le="1"\} .*$`))
			Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*,le="10"\} .*$`))
			Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*,le="30"\} .*$`))
			Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*,le="\+Inf"\} .*$`))
		})

		DescribeTable(
			"Counts correctly",
			func(count int) {
				// Prepare the handler:
				handler = wrapper(RespondWith(http.StatusOK, nil))

				// Send the requests:
				for i := 0; i < count; i++ {
					Send(http.MethodGet, "/")
				}

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_duration_count\{.*\} %d$`, count))
			},
			Entry("One", 1),
			Entry("Two", 2),
			Entry("Trhee", 3),
		)

		DescribeTable(
			"Includes method label",
			func(method string) {
				// Prepare the handler:
				handler = wrapper(RespondWith(http.StatusOK, nil))

				// Send the requests:
				Send(method, "/")

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*method="%s".*\} .*$`, method))
				Expect(metrics).To(MatchLine(`^\w+_request_duration_sum\{.*method="%s".*\} .*$`, method))
				Expect(metrics).To(MatchLine(`^\w+_request_duration_count\{.*method="%s".*\} .*$`, method))
			},
			Entry("GET", http.MethodGet),
			Entry("POST", http.MethodPost),
			Entry("PATCH", http.MethodPatch),
			Entry("DELETE", http.MethodDelete),
		)

		DescribeTable(
			"Includes path label",
			func(path, label string) {
				// Prepare the handler:
				handler = wrapper(RespondWith(http.StatusOK, nil))

				// Send the requests:
				Send(http.MethodGet, path)

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*path="%s".*\} .*$`, label))
				Expect(metrics).To(MatchLine(`^\w+_request_duration_sum\{.*path="%s".*\} .*$`, label))
				Expect(metrics).To(MatchLine(`^\w+_request_duration_count\{.*path="%s".*\} .*$`, label))
			},
			Entry(
				"Empty",
				"",
				"/",
			),
			Entry(
				"One slash",
				"/",
				"/",
			),
			Entry(
				"Two slashes",
				"//",
				"/",
			),
			Entry(
				"Tree slashes",
				"///",
				"/",
			),
			Entry(
				"Unknown root",
				"/junk/",
				"/-",
			),
			Entry(
				"Service root",
				"/my",
				"/my",
			),
			Entry(
				"Unknown service root",
				"/junk",
				"/-",
			),
			Entry(
				"Version root",
				"/my/v1",
				"/my/v1",
			),
			Entry(
				"Unknown version root",
				"/junk/v1",
				"/-",
			),
			Entry(
				"Collection",
				"/my/v1/resources",
				"/my/v1/resources",
			),
			Entry(
				"Unknown collection",
				"/my/v1/junk",
				"/-",
			),
			Entry(
				"Collection item",
				"/my/v1/resources/123",
				"/my/v1/resources/-",
			),
			Entry(
				"Collection item action",
				"/my/v1/resources/123/action",
				"/my/v1/resources/-/action",
			),
			Entry(
				"Unknown collection item action",
				"/my/v1/resources/123/junk",
				"/-",
			),
			Entry(
				"Subcollection",
				"/my/v1/resources/123/groups",
				"/my/v1/resources/-/groups",
			),
			Entry(
				"Unknown subcollection",
				"/my/v1/resources/123/junks",
				"/-",
			),
			Entry(
				"Subcollection item",
				"/my/v1/resources/123/groups/456",
				"/my/v1/resources/-/groups/-",
			),
			Entry(
				"Too long",
				"/my/v1/resources/123/groups/456/junk",
				"/-",
			),
			Entry(
				"Unknown path",
				"/your/path",
				"/-",
			),
		)

		DescribeTable(
			"Includes code label",
			func(code int) {
				// Prepare the handler:
				handler = wrapper(RespondWith(code, nil))

				// Send the requests:
				Send(http.MethodGet, "/")

				// Verify the metrics:
				metrics := server.Metrics()
				Expect(metrics).To(MatchLine(`^\w+_request_duration_bucket\{.*code="%d".*\} .*$`, code))
				Expect(metrics).To(MatchLine(`^\w+_request_duration_sum\{.*code="%d".*\} .*$`, code))
				Expect(metrics).To(MatchLine(`^\w+_request_duration_count\{.*code="%d".*\} .*$`, code))
			},
			Entry("200", http.StatusOK),
			Entry("201", http.StatusCreated),
			Entry("202", http.StatusAccepted),
			Entry("401", http.StatusUnauthorized),
			Entry("404", http.StatusNotFound),
			Entry("500", http.StatusInternalServerError),
		)
	})
})

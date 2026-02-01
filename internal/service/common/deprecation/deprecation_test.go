/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package deprecation_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/deprecation"
)

// Test fixtures - these are used across multiple tests
var testSunsetDate = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func testField(endpointPath, schema, fieldName, migrationGuide string) deprecation.Field {
	return deprecation.Field{
		EndpointPath:   endpointPath,
		Schema:         schema,
		FieldName:      fieldName,
		SunsetDate:     testSunsetDate,
		MigrationGuide: migrationGuide,
	}
}

var _ = Describe("Deprecation Registry", func() {

	Describe("GetFieldsForEndpoint", func() {
		BeforeEach(func() {
			deprecation.SetRegistry([]deprecation.Field{
				testField("/api/v1/widgets", "Widget", "legacyField", "/docs/widgets.md"),
				testField("/api/v1/widgets", "Widget", "oldName", "/docs/widgets.md"),
				testField("/api/v1/gadgets", "Gadget", "deprecatedAttr", "/docs/gadgets.md"),
			})
		})

		It("returns fields matching the endpoint path", func() {
			fields := deprecation.GetFieldsForEndpoint("/api/v1/widgets")

			Expect(fields).To(HaveLen(2))
			for _, field := range fields {
				Expect(field.Schema).To(Equal("Widget"))
			}
		})

		It("returns fields for sub-paths (e.g., /widgets/{id})", func() {
			fields := deprecation.GetFieldsForEndpoint("/api/v1/widgets/123-456")

			Expect(fields).To(HaveLen(2))
			for _, field := range fields {
				Expect(field.Schema).To(Equal("Widget"))
			}
		})

		It("returns different fields for different endpoints", func() {
			fields := deprecation.GetFieldsForEndpoint("/api/v1/gadgets")

			Expect(fields).To(HaveLen(1))
			Expect(fields[0].Schema).To(Equal("Gadget"))
		})

		It("returns empty for paths without deprecations", func() {
			fields := deprecation.GetFieldsForEndpoint("/api/v1/newstuff")

			Expect(fields).To(BeEmpty())
		})

		It("returns empty for unrelated paths", func() {
			fields := deprecation.GetFieldsForEndpoint("/health")

			Expect(fields).To(BeEmpty())
		})
	})

	Describe("GetFieldsForEndpoint with empty registry", func() {
		BeforeEach(func() {
			deprecation.SetRegistry([]deprecation.Field{})
		})

		It("returns empty for any path", func() {
			Expect(deprecation.GetFieldsForEndpoint("/api/v1/anything")).To(BeEmpty())
			Expect(deprecation.GetFieldsForEndpoint("/health")).To(BeEmpty())
		})
	})

	Describe("GetSunsetDate", func() {
		It("returns earliest date from multiple fields (sorted via SetRegistry)", func() {
			// SetRegistry sorts fields by SunsetDate, so earliest is first
			deprecation.SetRegistry([]deprecation.Field{
				{EndpointPath: "/test", SunsetDate: time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)},
				{EndpointPath: "/test", SunsetDate: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
				{EndpointPath: "/test", SunsetDate: time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)},
			})

			fields := deprecation.GetFieldsForEndpoint("/test")
			earliest := deprecation.GetSunsetDate(fields)

			Expect(earliest).To(Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)))
		})

		It("returns the only date when single field", func() {
			deprecation.SetRegistry([]deprecation.Field{
				{EndpointPath: "/single", SunsetDate: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
			})

			fields := deprecation.GetFieldsForEndpoint("/single")
			earliest := deprecation.GetSunsetDate(fields)

			Expect(earliest).To(Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)))
		})

		It("returns zero time for empty slice", func() {
			deprecation.SetRegistry([]deprecation.Field{})

			fields := deprecation.GetFieldsForEndpoint("/nonexistent")
			earliest := deprecation.GetSunsetDate(fields)

			Expect(earliest.IsZero()).To(BeTrue())
		})
	})

	Describe("GetMigrationGuide", func() {
		It("returns the first migration guide", func() {
			fields := []deprecation.Field{
				{MigrationGuide: "/docs/guide1.md"},
				{MigrationGuide: "/docs/guide2.md"},
			}

			guide := deprecation.GetMigrationGuide(fields)

			Expect(guide).To(Equal("/docs/guide1.md"))
		})

		It("skips fields without migration guide", func() {
			fields := []deprecation.Field{
				{MigrationGuide: ""},
				{MigrationGuide: "/docs/guide.md"},
			}

			guide := deprecation.GetMigrationGuide(fields)

			Expect(guide).To(Equal("/docs/guide.md"))
		})

		It("returns empty string when no guides available", func() {
			fields := []deprecation.Field{
				{MigrationGuide: ""},
			}

			guide := deprecation.GetMigrationGuide(fields)

			Expect(guide).To(BeEmpty())
		})
	})

	Describe("HasDeprecatedFields", func() {
		BeforeEach(func() {
			deprecation.SetRegistry([]deprecation.Field{
				testField("/api/v1/deprecated", "DeprecatedSchema", "oldField", "/docs/deprecated.md"),
			})
		})

		It("returns true for endpoints with deprecated fields", func() {
			Expect(deprecation.HasDeprecatedFields("/api/v1/deprecated")).To(BeTrue())
		})

		It("returns true for sub-paths of deprecated endpoints", func() {
			Expect(deprecation.HasDeprecatedFields("/api/v1/deprecated/123")).To(BeTrue())
		})

		It("returns false for endpoints without deprecated fields", func() {
			Expect(deprecation.HasDeprecatedFields("/api/v1/not-deprecated")).To(BeFalse())
		})
	})

	Describe("DaysUntilSunset", func() {
		It("returns positive days for future date", func() {
			futureDate := time.Now().AddDate(0, 0, 30) // 30 days from now

			days := deprecation.DaysUntilSunset(futureDate)

			Expect(days).To(BeNumerically(">=", 29))
			Expect(days).To(BeNumerically("<=", 30))
		})

		It("returns negative days for past date", func() {
			pastDate := time.Now().AddDate(0, 0, -10) // 10 days ago

			days := deprecation.DaysUntilSunset(pastDate)

			Expect(days).To(BeNumerically("<=", -9))
			Expect(days).To(BeNumerically(">=", -10))
		})

		It("returns zero for today", func() {
			today := time.Now()

			days := deprecation.DaysUntilSunset(today)

			Expect(days).To(BeNumerically(">=", -1))
			Expect(days).To(BeNumerically("<=", 0))
		})
	})
})

var _ = Describe("Deprecation Middleware", func() {

	Describe("HeadersMiddleware", func() {
		var (
			handler    http.Handler
			middleware deprecation.Middleware
			recorder   *httptest.ResponseRecorder
		)

		BeforeEach(func() {
			// Set up test registry with known values
			deprecation.SetRegistry([]deprecation.Field{
				{
					EndpointPath:   "/api/v1/deprecated-resource",
					Schema:         "DeprecatedResource",
					FieldName:      "oldField",
					SunsetDate:     testSunsetDate,
					MigrationGuide: "/docs/migration.md",
				},
			})

			// Create a simple handler that returns 200 OK
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})
			middleware = deprecation.HeadersMiddleware("https://example.com")
			recorder = httptest.NewRecorder()
		})

		It("adds deprecation headers for deprecated endpoints", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/deprecated-resource", nil)

			middleware(handler).ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get("Deprecation")).To(Equal("true"))
			Expect(recorder.Header().Get("Sunset")).ToNot(BeEmpty())
			Expect(recorder.Header().Get("Link")).To(ContainSubstring("rel=\"deprecation\""))
			Expect(recorder.Header().Get("Link")).To(ContainSubstring("https://example.com"))
		})

		It("adds deprecation headers for sub-paths of deprecated endpoints", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/deprecated-resource/123", nil)

			middleware(handler).ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get("Deprecation")).To(Equal("true"))
			Expect(recorder.Header().Get("Sunset")).ToNot(BeEmpty())
		})

		It("does NOT add deprecation headers for non-deprecated endpoints", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/new-resource", nil)

			middleware(handler).ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get("Deprecation")).To(BeEmpty())
			Expect(recorder.Header().Get("Sunset")).To(BeEmpty())
			Expect(recorder.Header().Get("Link")).To(BeEmpty())
		})

		It("does NOT add deprecation headers for health endpoint", func() {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)

			middleware(handler).ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get("Deprecation")).To(BeEmpty())
		})

		It("formats Sunset header as HTTP date", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/deprecated-resource", nil)

			middleware(handler).ServeHTTP(recorder, req)

			sunset := recorder.Header().Get("Sunset")
			Expect(sunset).ToNot(BeEmpty())
			// Verify it's a valid HTTP date format (e.g., "Mon, 01 Jun 2026 00:00:00 GMT")
			_, err := time.Parse(http.TimeFormat, sunset)
			Expect(err).ToNot(HaveOccurred())
		})

		It("formats Link header per RFC 8288", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/deprecated-resource", nil)

			middleware(handler).ServeHTTP(recorder, req)

			link := recorder.Header().Get("Link")
			Expect(link).To(MatchRegexp(`^<https://example\.com/docs/migration\.md>; rel="deprecation"$`))
		})

		It("does not add Link header when baseURL is empty", func() {
			emptyBaseMiddleware := deprecation.HeadersMiddleware("")
			req := httptest.NewRequest(http.MethodGet, "/api/v1/deprecated-resource", nil)

			emptyBaseMiddleware(handler).ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Deprecation")).To(Equal("true"))
			Expect(recorder.Header().Get("Link")).To(BeEmpty())
		})
	})

	Describe("HeadersMiddleware with empty registry", func() {
		BeforeEach(func() {
			deprecation.SetRegistry([]deprecation.Field{})
		})

		It("does not add any deprecation headers", func() {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			middleware := deprecation.HeadersMiddleware("https://example.com")
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/any/path", nil)

			middleware(handler).ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Deprecation")).To(BeEmpty())
			Expect(recorder.Header().Get("Sunset")).To(BeEmpty())
			Expect(recorder.Header().Get("Link")).To(BeEmpty())
		})
	})
})

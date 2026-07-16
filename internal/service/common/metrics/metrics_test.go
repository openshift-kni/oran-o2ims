/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metrics

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NormalizePath", func() {
	DescribeTable("replaces UUID segments with {id}",
		func(input, expected string) {
			Expect(NormalizePath(input)).To(Equal(expected))
		},
		Entry("collection endpoint unchanged",
			"/o2ims-infrastructureInventory/v1/resourcePools",
			"/o2ims-infrastructureInventory/v1/resourcePools",
		),
		Entry("single UUID replaced",
			"/o2ims-infrastructureInventory/v1/resourcePools/bf3f8f2e-6f37-4882-96ad-c0b9cef6fc04",
			"/o2ims-infrastructureInventory/v1/resourcePools/{id}",
		),
		Entry("nested resource with two UUIDs",
			"/o2ims-infrastructureInventory/v1/resourcePools/bf3f8f2e-6f37-4882-96ad-c0b9cef6fc04/resources/a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"/o2ims-infrastructureInventory/v1/resourcePools/{id}/resources/{id}",
		),
		Entry("uppercase UUID replaced",
			"/o2ims-infrastructureCluster/v1/nodeClusters/BF3F8F2E-6F37-4882-96AD-C0B9CEF6FC04",
			"/o2ims-infrastructureCluster/v1/nodeClusters/{id}",
		),
		Entry("path without UUIDs unchanged",
			"/o2ims-infrastructureMonitoring/v1/alarms",
			"/o2ims-infrastructureMonitoring/v1/alarms",
		),
		Entry("empty path unchanged", "", ""),
		Entry("root path unchanged", "/", "/"),
	)
})

var _ = Describe("RegisterMetricsHandler", func() {
	It("registers the handler at /metrics with middleware applied in correct order", func() {
		mux := http.NewServeMux()
		var callOrder []string

		authn := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callOrder = append(callOrder, "authn")
				next.ServeHTTP(w, r)
			})
		}
		authz := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callOrder = append(callOrder, "authz")
				next.ServeHTTP(w, r)
			})
		}

		RegisterMetricsHandler(mux, authn, authz)

		req := httptest.NewRequest(http.MethodGet, MetricsPath, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(callOrder).To(Equal([]string{"authn", "authz"}))
	})

	It("returns 404 for paths other than /metrics", func() {
		mux := http.NewServeMux()
		noop := func(next http.Handler) http.Handler { return next }

		RegisterMetricsHandler(mux, noop, noop)

		req := httptest.NewRequest(http.MethodGet, "/not-metrics", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("blocks unauthenticated requests when authn rejects", func() {
		mux := http.NewServeMux()

		authn := func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			})
		}
		authz := func(next http.Handler) http.Handler { return next }

		RegisterMetricsHandler(mux, authn, authz)

		req := httptest.NewRequest(http.MethodGet, MetricsPath, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("blocks unauthorized requests when authz rejects", func() {
		mux := http.NewServeMux()

		authn := func(next http.Handler) http.Handler { return next }
		authz := func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			})
		}

		RegisterMetricsHandler(mux, authn, authz)

		req := httptest.NewRequest(http.MethodGet, MetricsPath, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusForbidden))
	})
})

var _ = Describe("AuthFailures counter", func() {
	It("is registered and can be incremented", func() {
		AuthFailures.WithLabelValues("test-service", "authn", "GET", "/test").Inc()

		metric, err := AuthFailures.GetMetricWithLabelValues("test-service", "authn", "GET", "/test")
		Expect(err).ToNot(HaveOccurred())
		Expect(metric).ToNot(BeNil())
	})
})

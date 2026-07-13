/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metrics

import (
	"net/http"
	"regexp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var uuidSegmentRe = regexp.MustCompile(
	`/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`,
)

// NormalizePath replaces UUID path segments with {id} to keep metric
// cardinality bounded. Paths without UUIDs pass through unchanged.
func NormalizePath(path string) string {
	return uuidSegmentRe.ReplaceAllString(path, "/{id}")
}

var AuthFailures *prometheus.CounterVec

func init() {
	AuthFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "o2ims_auth_failures_total",
			Help: "Total authentication/authorization failures by service, type, method, and path",
		},
		[]string{"service", "type", "method", "path"},
	)
	prometheus.MustRegister(AuthFailures)
}

// RegisterMetricsHandler registers the /metrics endpoint on the given mux,
// wrapping it with authentication and authorization middleware.
func RegisterMetricsHandler(mux *http.ServeMux, authn, authz func(http.Handler) http.Handler) {
	var handler http.Handler = promhttp.Handler()
	handler = authz(handler)
	handler = authn(handler)
	mux.Handle("/metrics", handler)
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package deprecation

import (
	"net/http"
)

// Middleware wraps an http.Handler
type Middleware = func(http.Handler) http.Handler

// HeadersMiddleware adds RFC 8594 deprecation headers to HTTP responses
// for endpoints that return deprecated fields.
//
// Headers added:
//   - Deprecation: true (or the deprecation date per RFC 8594)
//   - Sunset: <date> (when the deprecated features will be removed)
//   - Link: <url>; rel="deprecation" (link to migration documentation)
//
// Reference: https://datatracker.ietf.org/doc/html/rfc8594
func HeadersMiddleware(docsBaseURL string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if this endpoint returns deprecated fields
			deprecatedFields := GetFieldsForEndpoint(r.URL.Path)

			if len(deprecatedFields) > 0 {
				// Get the earliest sunset date among all deprecated fields
				sunsetDate := GetSunsetDate(deprecatedFields)

				// RFC 8594: Deprecation header
				// Can be "true" or a date when deprecation started
				w.Header().Set("Deprecation", "true")

				// RFC 8594: Sunset header - when the feature will be removed
				if !sunsetDate.IsZero() {
					w.Header().Set("Sunset", sunsetDate.Format(http.TimeFormat))
				}

				// RFC 8288: Link header with rel="deprecation"
				// Points to migration documentation
				migrationGuide := GetMigrationGuide(deprecatedFields)
				if migrationGuide != "" && docsBaseURL != "" {
					link := "<" + docsBaseURL + migrationGuide + ">; rel=\"deprecation\""
					w.Header().Set("Link", link)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

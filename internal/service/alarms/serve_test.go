/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package alarms

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"
	alarmapi "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
)

// TestWrapAlarmHandler_VersionHeaderScope verifies the path-scoped Version response header added by
// wrapAlarmHandler: it is present on public Monitoring v2 responses (success and handled error) and
// on the unversioned discovery path in both its bare and trailing-slash forms, and absent on the
// internal ingestion routes and /metrics.
func TestWrapAlarmHandler_VersionHeaderScope(t *testing.T) {
	g := NewWithT(t)

	// inner returns a handled-error status when ?fail=1 is set, otherwise 200. The Version header
	// is set by the outer middleware before inner runs, so it must be present regardless of status.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fail") == "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := wrapAlarmHandler(inner)

	call := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	withHeader := []struct {
		name string
		path string
	}{
		{"v2 success", "/O2ims_infrastructureMonitoring/v2/alarms"},
		{"v2 handled error", "/O2ims_infrastructureMonitoring/v2/alarms?fail=1"},
		{"v2 subresource", "/O2ims_infrastructureMonitoring/v2/alarmSubscriptions/123"},
		{"unversioned discovery", "/O2ims_infrastructureMonitoring/api_versions"},
		{"unversioned discovery trailing slash", "/O2ims_infrastructureMonitoring/api_versions/"},
	}
	for _, tc := range withHeader {
		rr := call(tc.path)
		g.Expect(rr.Header().Get("Version")).To(Equal(api.CurrentVersion), "expected Version header for %s", tc.name)
	}

	// The handled-error case also returns the non-200 status, confirming the header is set
	// independently of the response status.
	g.Expect(call("/O2ims_infrastructureMonitoring/v2/alarms?fail=1").Code).To(Equal(http.StatusBadRequest))

	withoutHeader := []struct {
		name string
		path string
	}{
		{"internal ingestion", "/internal/v1/caas-alerts"},
		{"metrics", "/metrics"},
		{"v2 prefix without a path-segment boundary", "/O2ims_infrastructureMonitoring/v20"},
	}
	for _, tc := range withoutHeader {
		rr := call(tc.path)
		g.Expect(rr.Header().Get("Version")).To(BeEmpty(), "expected no Version header for %s", tc.name)
	}
}

// newAlarmsTestRouter assembles the production HTTP stack (minus auth) around the alarms server:
// GetSwagger -> AlarmsServer with a mock repo -> NewStrictHandlerWithOptions -> HandlerWithOptions
// with OpenAPI request validation and the response filter -> wrapAlarmHandler. It mirrors the
// provisioning envtest router assembly but needs no envtest, since the exercised routes are the
// discovery endpoints (no repo access) and a single list route backed by the mock.
func newAlarmsTestRouter(t *testing.T) (http.Handler, *generated.MockAlarmRepositoryInterface) {
	t.Helper()
	g := NewWithT(t)

	swagger, err := alarmapi.GetSwagger()
	g.Expect(err).ToNot(HaveOccurred())

	ctrl := gomock.NewController(t)
	mockRepo := generated.NewMockAlarmRepositoryInterface(ctrl)
	server := &api.AlarmsServer{Config: &api.AlarmsServerConfig{}, AlarmsRepository: mockRepo}

	strictHandler := alarmapi.NewStrictHandlerWithOptions(server, nil,
		alarmapi.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  middleware.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: middleware.GetOranRespErrFunc(),
		},
	)

	logger := slog.New(slog.DiscardHandler)
	filterAdapter, err := middleware.NewFilterAdapterFromSwagger(logger, swagger)
	g.Expect(err).ToNot(HaveOccurred())

	baseRouter := http.NewServeMux()
	alarmapi.HandlerWithOptions(strictHandler, alarmapi.StdHTTPServerOptions{
		BaseRouter: baseRouter,
		Middlewares: []alarmapi.MiddlewareFunc{
			middleware.OpenAPIValidation(swagger),
			middleware.ResponseFilter(filterAdapter),
		},
		ErrorHandlerFunc: middleware.GetOranReqErrFunc(),
	})

	return wrapAlarmHandler(baseRouter), mockRepo
}

// TestAlarmsRouter_DiscoveryAndV2Routing drives the full HTTP router to verify the Monitoring v2
// cutover end to end: both discovery endpoints advertise only version 2.2.0 with a trailing-slash
// uriPrefix, a representative v2 route is served, and the retired v1 monitoring path is gone (404).
func TestAlarmsRouter_DiscoveryAndV2Routing(t *testing.T) {
	g := NewWithT(t)
	handler, mockRepo := newAlarmsTestRouter(t)

	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	type apiVersionsResp struct {
		ApiVersions []struct {
			Version string `json:"version"`
		} `json:"apiVersions"`
		UriPrefix string `json:"uriPrefix"`
	}

	// Both discovery endpoints return hardcoded version info and need no repo setup.
	discoveryPaths := []struct {
		name string
		path string
	}{
		{"unversioned discovery", "/O2ims_infrastructureMonitoring/api_versions"},
		{"v2 minor-version discovery", "/O2ims_infrastructureMonitoring/v2/api_versions"},
	}
	for _, tc := range discoveryPaths {
		rr := call(http.MethodGet, tc.path)
		g.Expect(rr.Code).To(Equal(http.StatusOK), "unexpected status for %s", tc.name)

		var body apiVersionsResp
		g.Expect(json.Unmarshal(rr.Body.Bytes(), &body)).To(Succeed(), "decoding %s", tc.name)
		g.Expect(body.ApiVersions).To(HaveLen(1), "%s should advertise a single version", tc.name)
		g.Expect(body.ApiVersions[0].Version).To(Equal(api.CurrentVersion), "%s version", tc.name)
		g.Expect(body.UriPrefix).To(HaveSuffix("/"), "%s uriPrefix must end with a trailing slash", tc.name)
	}

	// A representative v2 route is served. Set the repo expectation so the handler returns an
	// empty list (200) rather than failing on a nil repo.
	mockRepo.EXPECT().
		GetAlarmEventRecords(gomock.Any()).
		Return([]models.AlarmEventRecord{}, nil)
	g.Expect(call(http.MethodGet, "/O2ims_infrastructureMonitoring/v2/alarms").Code).To(Equal(http.StatusOK))

	// The retired v1 monitoring route is no longer registered.
	g.Expect(call(http.MethodGet, "/O2ims_infrastructureMonitoring/v1/alarms").Code).To(Equal(http.StatusNotFound))
}

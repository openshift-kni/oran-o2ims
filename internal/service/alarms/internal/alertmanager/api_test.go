// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package alertmanager_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TimePtr(t time.Time) *time.Time {
	return &t
}

func StringPtr(s string) *string {
	return &s
}

var _ = Describe("Alertmanager API Client", func() {
	var (
		ctx           context.Context
		fakeClient    client.Client
		ctrl          *gomock.Controller
		mockRepo      *generated.MockAlarmRepositoryInterface
		infra         *infrastructure.Infrastructure
		amClient      *alertmanager.AMClient
		mockAMServer  *httptest.Server
		testAPIAlerts []alertmanager.APIAlert
		tempTokenFile string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = generated.NewMockAlarmRepositoryInterface(ctrl)

		// Create mock Alertmanager server
		mockAMServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request has correct path
			Expect(r.URL.Path).To(Equal("/api/v2/alerts"))
			// Verify auth token is present
			authHeader := r.Header.Get("Authorization")
			Expect(authHeader).To(Equal("Bearer fake-token"))

			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(testAPIAlerts)
			Expect(err).To(BeNil())
		}))

		// Create temporary token file for testing
		tempDir := GinkgoT().TempDir()
		tempTokenFile = tempDir + "/token"
		err := os.WriteFile(tempTokenFile, []byte("fake-token"), 0o600)
		Expect(err).NotTo(HaveOccurred())

		// Extract server certificate for use in our fake CA file
		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: mockAMServer.Certificate().Raw,
		})

		// Create temporary CA file with the test server's certificate
		tempCAFile := tempDir + "/ca.crt"
		err = os.WriteFile(tempCAFile, certPEM, 0o600)
		Expect(err).NotTo(HaveOccurred())

		// Extract server hostname from test server URL without the scheme
		serverHost := mockAMServer.URL[8:] // Skip "https://"

		// Set environment variables to point to our temp files and test server
		os.Setenv("ALARMS_SERVER_TOKEN_FILE", tempTokenFile)
		os.Setenv("ALARMS_SERVER_CA_FILE", tempCAFile)
		os.Setenv("ALARMS_SERVER_AM_HOST", serverHost)

		DeferCleanup(func() {
			os.Unsetenv("ALARMS_SERVER_TOKEN_FILE")
			os.Unsetenv("ALARMS_SERVER_CA_FILE")
			os.Unsetenv("ALARMS_SERVER_AM_HOST")
		})

		// Set up minimal fake k8s client (only needed for infrastructure)
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		infra = &infrastructure.Infrastructure{
			ClusterServer:  &infrastructure.ClusterServer{},
			ResourceServer: &infrastructure.ResourceServer{},
		}

		amClient = alertmanager.NewAlertmanagerClient(fakeClient, mockRepo, infra)

		// Set up test alerts
		testAPIAlerts = []alertmanager.APIAlert{
			{
				Annotations: &map[string]string{
					"summary":     "Test Alert",
					"description": "This is a test alert",
				},
				Labels: &map[string]string{
					"alertname": "TestAlert",
					"severity":  "critical",
					"receiver":  alertmanager.OranReceiverName,
				},
				StartsAt:     TimePtr(time.Now().Add(-time.Hour)),
				EndsAt:       TimePtr(time.Now().Add(time.Hour)),
				Fingerprint:  StringPtr("fingerprint1"),
				GeneratorURL: StringPtr("http://prometheus/graph?g0.expr=..."),
				Status: &alertmanager.Status{
					State: "active",
				},
			},
		}
	})

	AfterEach(func() {
		mockAMServer.Close()
		ctrl.Finish()
	})

	Describe("SyncAlerts", func() {
		It("should successfully sync alerts from Alertmanager", func() {
			mockRepo.EXPECT().
				WithTransaction(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, fn func(tx pgx.Tx) error) error {
					// Execute the callback with a nil transaction
					return fn(nil)
				}).Times(1)

			// Expectations from inside the transaction
			mockRepo.EXPECT().
				UpsertAlarmEventCaaSRecord(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil).Times(1)
			mockRepo.EXPECT().
				ResolveStaleAlarmEventCaaSRecord(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil).Times(1)

			err := amClient.SyncAlerts(ctx)

			// Then
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle empty alerts gracefully", func() {
			testAPIAlerts = []alertmanager.APIAlert{}
			err := amClient.SyncAlerts(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle API errors appropriately", func() {
			// Close the server to simulate connection error
			mockAMServer.Close()

			err := amClient.SyncAlerts(ctx)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get alerts"))
		})

		It("should handle errors from saving alarms", func() {
			// We expect the save to fail
			mockRepo.EXPECT().
				WithTransaction(gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("failed to handle alerts")).Times(1)

			err := amClient.SyncAlerts(ctx)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to handle alerts"))
		})
	})

	Describe("RunAlertSyncScheduler", func() {
		It("should schedule alert syncs at regular intervals", func() {
			// Given a short interval for testing
			interval := 100 * time.Millisecond

			mockRepo.EXPECT().
				WithTransaction(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, fn func(tx pgx.Tx) error) error {
					/// Add GinkgoRecover() inside the goroutine
					defer GinkgoRecover()
					// Execute the callback with a nil transaction
					return fn(nil)
				}).AnyTimes()

			// Expectations from inside the transaction
			mockRepo.EXPECT().
				UpsertAlarmEventCaaSRecord(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil).AnyTimes()
			mockRepo.EXPECT().
				ResolveStaleAlarmEventCaaSRecord(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil).AnyTimes()

			// Create a context with cancel to stop the scheduler
			ctxWithCancel, cancel := context.WithCancel(ctx)

			// Start scheduler in a separate goroutine
			errChan := make(chan error, 1)
			go func() {
				// Add GinkgoRecover() inside the goroutine
				defer GinkgoRecover()
				errChan <- amClient.RunAlertSyncScheduler(ctxWithCancel, interval)
			}()

			// Let it run for a bit
			time.Sleep(250 * time.Millisecond)

			// Cancel context to stop scheduler
			cancel()

			// Check for any errors
			var err error
			select {
			case err = <-errChan:
				// Got a result
			case <-time.After(500 * time.Millisecond):
				// Timed out waiting for result
				err = fmt.Errorf("timed out waiting for scheduler to return")
			}

			Expect(err).NotTo(HaveOccurred())
		})
	})
})

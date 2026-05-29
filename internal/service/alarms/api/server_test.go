/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"
	alarmapi "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

type mockClientProvider struct {
	client *http.Client
}

func (m *mockClientProvider) NewClient(_ context.Context, _ commonapi.AuthType) (*http.Client, error) {
	return m.client, nil
}

type mockSubscriptionEventHandler struct {
	provider notifier.ClientProvider
}

func (m *mockSubscriptionEventHandler) SubscriptionEvent(_ context.Context, _ *notifier.SubscriptionEvent) {
}

func (m *mockSubscriptionEventHandler) GetClientFactory() notifier.ClientProvider {
	return m.provider
}

var _ = Describe("AlarmsServer", func() {
	var (
		ctrl     *gomock.Controller
		mockRepo *generated.MockAlarmRepositoryInterface
		server   *api.AlarmsServer
		ctx      context.Context
		testUUID uuid.UUID
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = generated.NewMockAlarmRepositoryInterface(ctrl)
		server = &api.AlarmsServer{AlarmsRepository: mockRepo}
		ctx = context.Background()
		testUUID = uuid.New()
	})

	Describe("GetAlarm", func() {
		When("alarm not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetAlarmEventRecord(ctx, testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.GetAlarm(ctx, alarmapi.GetAlarmRequestObject{
					AlarmEventRecordId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.GetAlarm404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
			})
		})

		When("alarm is found", func() {
			It("returns 200 response with alarm", func() {
				mockRepo.EXPECT().
					GetAlarmEventRecord(ctx, testUUID).
					Return(&models.AlarmEventRecord{AlarmEventRecordID: testUUID}, nil)

				resp, err := server.GetAlarm(ctx, alarmapi.GetAlarmRequestObject{
					AlarmEventRecordId: testUUID,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(alarmapi.GetAlarm200JSONResponse{}))
			})
		})

		When("repository is unavailable", func() {
			It("returns error", func() {
				mockRepo.EXPECT().
					GetAlarmEventRecord(ctx, testUUID).
					Return(nil, fmt.Errorf("db error"))

				resp, err := server.GetAlarm(ctx, alarmapi.GetAlarmRequestObject{
					AlarmEventRecordId: testUUID,
				})

				Expect(err).To(HaveOccurred())
				Expect(resp).To(BeNil())
			})
		})
	})

	Describe("CreateSubscription", func() {
		When("GlobalCloudID is not set", func() {
			It("returns 409 conflict", func() {
				server.GlobalCloudID = uuid.Nil
				resp, err := server.CreateSubscription(ctx, alarmapi.CreateSubscriptionRequestObject{
					Body: &alarmapi.AlarmSubscriptionInfo{
						Callback: "https://example.com/callback",
					},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(alarmapi.CreateSubscription409ApplicationProblemPlusJSONResponse{}))
			})
		})

		When("validation fails due to invalid callback URL scheme", func() {
			It("returns 400 without reflecting the callback in response", func() {
				server.GlobalCloudID = uuid.New()
				server.SubscriptionEventHandler = &mockSubscriptionEventHandler{}

				callbackURL := "http://malicious.example.com/callback?secret=token123"
				resp, err := server.CreateSubscription(ctx, alarmapi.CreateSubscriptionRequestObject{
					Body: &alarmapi.AlarmSubscriptionInfo{
						Callback: callbackURL,
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.CreateSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(problemResp.AdditionalAttributes).To(BeNil())
				Expect(problemResp.Detail).ToNot(ContainSubstring(callbackURL))
			})
		})

		When("validation fails due to unreachable callback URL", func() {
			It("returns 400 without reflecting the callback URL in Detail", func() {
				server.GlobalCloudID = uuid.New()
				callbackURL := "https://192.0.2.1/callback?secret=token123"
				shortTimeoutClient := &http.Client{Timeout: 1}

				server.SubscriptionEventHandler = &mockSubscriptionEventHandler{
					provider: &mockClientProvider{client: shortTimeoutClient},
				}

				resp, err := server.CreateSubscription(ctx, alarmapi.CreateSubscriptionRequestObject{
					Body: &alarmapi.AlarmSubscriptionInfo{
						Callback: callbackURL,
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.CreateSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(problemResp.Detail).ToNot(ContainSubstring(callbackURL))
				Expect(problemResp.Detail).ToNot(ContainSubstring("192.0.2.1"))
				Expect(problemResp.Detail).ToNot(ContainSubstring("secret=token123"))
			})
		})

		When("repository returns unique_callback error", func() {
			It("returns 400 without reflecting the callback in response", func() {
				server.GlobalCloudID = uuid.New()
				ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}))
				defer ts.Close()

				server.SubscriptionEventHandler = &mockSubscriptionEventHandler{
					provider: &mockClientProvider{client: ts.Client()},
				}

				mockRepo.EXPECT().
					CreateAlarmSubscription(ctx, gomock.Any()).
					Return(nil, &pgconn.PgError{Code: "23505", ConstraintName: "unique_callback"})

				resp, err := server.CreateSubscription(ctx, alarmapi.CreateSubscriptionRequestObject{
					Body: &alarmapi.AlarmSubscriptionInfo{
						Callback: ts.URL + "/callback",
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.CreateSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(problemResp.Detail).To(Equal("callback value must be unique"))
				Expect(problemResp.AdditionalAttributes).To(BeNil())
			})
		})
	})
})

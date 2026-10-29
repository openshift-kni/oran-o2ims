/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oapi-codegen/nullable"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"
	alarmapi "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	svcapi "github.com/openshift-kni/oran-o2ims/internal/service/common/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
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
	events   []*notifier.SubscriptionEvent
}

func (m *mockSubscriptionEventHandler) SubscriptionEvent(_ context.Context, event *notifier.SubscriptionEvent) {
	m.events = append(m.events, event)
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
		server = &api.AlarmsServer{Config: &api.AlarmsServerConfig{}, AlarmsRepository: mockRepo}
		ctx = context.Background()
		testUUID = uuid.New()
	})

	Describe("GetAlarm", func() {
		When("alarm not found", func() {
			It("returns 404 response", func() {
				mockRepo.EXPECT().
					GetAlarmEventRecord(gomock.Any(), testUUID).
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
					GetAlarmEventRecord(gomock.Any(), testUUID).
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
					GetAlarmEventRecord(gomock.Any(), testUUID).
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
				server.Config.SmoURL = "https://192.0.2.1"
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

				server.Config.SmoURL = ts.URL
				savedResolver := svcapi.DefaultResolver
				svcapi.DefaultResolver = &svcapi.TestResolver{Addrs: map[string][]string{
					"127.0.0.1": {"203.0.113.1"},
				}}
				DeferCleanup(func() { svcapi.DefaultResolver = savedResolver })

				server.SubscriptionEventHandler = &mockSubscriptionEventHandler{
					provider: &mockClientProvider{client: ts.Client()},
				}

				mockRepo.EXPECT().
					CreateAlarmSubscription(gomock.Any(), gomock.Any()).
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

	Describe("PatchSubscription", func() {
		var eventHandler *mockSubscriptionEventHandler

		BeforeEach(func() {
			eventHandler = &mockSubscriptionEventHandler{}
			server.SubscriptionEventHandler = eventHandler
		})

		When("callback is explicitly null", func() {
			It("returns 400 and does not update or notify", func() {
				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						Callback: nullable.NewNullNullable[string](),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.PatchSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(eventHandler.events).To(BeEmpty())
			})
		})

		When("the body is empty", func() {
			It("returns 200 with the current subscription and does not update or notify", func() {
				mockRepo.EXPECT().
					GetAlarmSubscription(gomock.Any(), testUUID).
					Return(&models.AlarmSubscription{SubscriptionID: testUUID, Callback: "http://x"}, nil)

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body:                &alarmapi.AlarmSubscriptionUpdate{},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(alarmapi.PatchSubscription200JSONResponse{}))
				Expect(eventHandler.events).To(BeEmpty())
			})
		})

		When("the body is empty and the subscription does not exist", func() {
			It("returns 404", func() {
				mockRepo.EXPECT().
					GetAlarmSubscription(gomock.Any(), testUUID).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body:                &alarmapi.AlarmSubscriptionUpdate{},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.PatchSubscription404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
				Expect(eventHandler.events).To(BeEmpty())
			})
		})

		When("eventFilter is explicitly null", func() {
			It("sets the column to NULL, returns 200, and notifies the notifier", func() {
				var captured map[string]any
				mockRepo.EXPECT().
					PatchAlarmSubscription(gomock.Any(), testUUID, gomock.Any()).
					DoAndReturn(func(_ context.Context, id uuid.UUID, fields map[string]any) (*models.AlarmSubscription, error) {
						captured = fields
						return &models.AlarmSubscription{SubscriptionID: id, Callback: "http://x"}, nil
					})

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						EventFilter: nullable.NewNullNullable[alarmapi.AlarmSubscriptionUpdateEventFilter](),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(alarmapi.PatchSubscription200JSONResponse{}))
				Expect(captured).To(HaveKey("EventFilter"))
				Expect(captured["EventFilter"]).To(BeNil())
				Expect(eventHandler.events).To(HaveLen(1))
				Expect(eventHandler.events[0].Updated).To(BeTrue())
				Expect(eventHandler.events[0].Subscription.SubscriptionID).To(Equal(testUUID))
			})
		})

		When("eventFilter and consumerSubscriptionId carry values", func() {
			It("sets both columns and returns 200", func() {
				var captured map[string]any
				mockRepo.EXPECT().
					PatchAlarmSubscription(gomock.Any(), testUUID, gomock.Any()).
					DoAndReturn(func(_ context.Context, id uuid.UUID, fields map[string]any) (*models.AlarmSubscription, error) {
						captured = fields
						return &models.AlarmSubscription{SubscriptionID: id, Callback: "http://x"}, nil
					})

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						EventFilter:            nullable.NewNullableWithValue(alarmapi.AlarmSubscriptionUpdateEventFilterCHANGE),
						ConsumerSubscriptionId: nullable.NewNullableWithValue("consumer-123"),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(alarmapi.PatchSubscription200JSONResponse{}))
				Expect(captured["EventFilter"]).To(Equal(string(alarmapi.AlarmSubscriptionUpdateEventFilterCHANGE)))
				Expect(captured["ConsumerSubscriptionID"]).To(Equal("consumer-123"))
				Expect(eventHandler.events).To(HaveLen(1))
			})
		})

		When("the subscription does not exist", func() {
			It("returns 404 and does not notify", func() {
				mockRepo.EXPECT().
					PatchAlarmSubscription(gomock.Any(), testUUID, gomock.Any()).
					Return(nil, svcutils.ErrNotFound)

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						EventFilter: nullable.NewNullableWithValue(alarmapi.AlarmSubscriptionUpdateEventFilterNEW),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.PatchSubscription404ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusNotFound))
				Expect(eventHandler.events).To(BeEmpty())
			})
		})

		// reachableCallbackServer stands up a TLS test server plus the SSRF resolver stub
		// so that ValidateCallbackURL accepts a callback pointing at it (mirrors the
		// CreateSubscription unique_callback setup).
		reachableCallbackServer := func() *httptest.Server {
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			DeferCleanup(ts.Close)
			server.Config.SmoURL = ts.URL
			savedResolver := svcapi.DefaultResolver
			svcapi.DefaultResolver = &svcapi.TestResolver{Addrs: map[string][]string{
				"127.0.0.1": {"203.0.113.1"},
			}}
			DeferCleanup(func() { svcapi.DefaultResolver = savedResolver })
			eventHandler.provider = &mockClientProvider{client: ts.Client()}
			return ts
		}

		When("callback carries a reachable value", func() {
			It("validates, sets the callback column, returns 200, and notifies", func() {
				ts := reachableCallbackServer()
				callback := ts.URL + "/callback"

				var captured map[string]any
				mockRepo.EXPECT().
					PatchAlarmSubscription(gomock.Any(), testUUID, gomock.Any()).
					DoAndReturn(func(_ context.Context, id uuid.UUID, fields map[string]any) (*models.AlarmSubscription, error) {
						captured = fields
						return &models.AlarmSubscription{SubscriptionID: id, Callback: callback}, nil
					})

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						Callback: nullable.NewNullableWithValue(callback),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(alarmapi.PatchSubscription200JSONResponse{}))
				Expect(captured).To(HaveKey("Callback"))
				Expect(captured["Callback"]).To(Equal(callback))
				Expect(eventHandler.events).To(HaveLen(1))
			})
		})

		When("callback has an invalid scheme", func() {
			It("returns 400 without updating or notifying", func() {
				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						Callback: nullable.NewNullableWithValue("http://malicious.example.com/callback"),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.PatchSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(eventHandler.events).To(BeEmpty())
			})
		})

		When("the repository reports a duplicate callback", func() {
			It("returns 400 and does not notify", func() {
				callback := reachableCallbackServer().URL + "/callback"

				mockRepo.EXPECT().
					PatchAlarmSubscription(gomock.Any(), testUUID, gomock.Any()).
					Return(nil, &pgconn.PgError{Code: "23505", ConstraintName: "unique_callback"})

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						Callback: nullable.NewNullableWithValue(callback),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				problemResp := resp.(alarmapi.PatchSubscription400ApplicationProblemPlusJSONResponse)
				Expect(problemResp.Status).To(Equal(http.StatusBadRequest))
				Expect(problemResp.Detail).To(Equal("callback value must be unique"))
				Expect(eventHandler.events).To(BeEmpty())
			})
		})

		When("consumerSubscriptionId is explicitly null", func() {
			It("sets the column to NULL, returns 200, and notifies", func() {
				var captured map[string]any
				mockRepo.EXPECT().
					PatchAlarmSubscription(gomock.Any(), testUUID, gomock.Any()).
					DoAndReturn(func(_ context.Context, id uuid.UUID, fields map[string]any) (*models.AlarmSubscription, error) {
						captured = fields
						return &models.AlarmSubscription{SubscriptionID: id, Callback: "http://x"}, nil
					})

				resp, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						ConsumerSubscriptionId: nullable.NewNullNullable[string](),
					},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(alarmapi.PatchSubscription200JSONResponse{}))
				Expect(captured).To(HaveKey("ConsumerSubscriptionID"))
				Expect(captured["ConsumerSubscriptionID"]).To(BeNil())
				Expect(eventHandler.events).To(HaveLen(1))
			})
		})

		When("two single-field patches are applied in sequence", func() {
			It("sends only the changed field on each call", func() {
				captured := make([]map[string]any, 0, 2)
				mockRepo.EXPECT().
					PatchAlarmSubscription(gomock.Any(), testUUID, gomock.Any()).
					DoAndReturn(func(_ context.Context, id uuid.UUID, fields map[string]any) (*models.AlarmSubscription, error) {
						captured = append(captured, fields)
						return &models.AlarmSubscription{SubscriptionID: id, Callback: "http://x"}, nil
					}).
					Times(2)

				_, err := server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						EventFilter: nullable.NewNullableWithValue(alarmapi.AlarmSubscriptionUpdateEventFilterCHANGE),
					},
				})
				Expect(err).ToNot(HaveOccurred())

				_, err = server.PatchSubscription(ctx, alarmapi.PatchSubscriptionRequestObject{
					AlarmSubscriptionId: testUUID,
					Body: &alarmapi.AlarmSubscriptionUpdate{
						ConsumerSubscriptionId: nullable.NewNullableWithValue("consumer-xyz"),
					},
				})
				Expect(err).ToNot(HaveOccurred())

				Expect(captured).To(HaveLen(2))
				// Each field is sent independently: no bleed-through between sequential patches.
				Expect(captured[0]).To(HaveKey("EventFilter"))
				Expect(captured[0]).ToNot(HaveKey("ConsumerSubscriptionID"))
				Expect(captured[1]).To(HaveKey("ConsumerSubscriptionID"))
				Expect(captured[1]).ToNot(HaveKey("EventFilter"))
				Expect(eventHandler.events).To(HaveLen(2))
			})
		})
	})

	Describe("PatchSubscription request validation", func() {
		// additionalProperties:false on AlarmSubscriptionUpdate is enforced by the shared
		// OpenAPI request-validation middleware, not the handler, so it is exercised here
		// against the generated spec.
		var handler http.Handler

		BeforeEach(func() {
			swagger, err := alarmapi.GetSwagger()
			Expect(err).ToNot(HaveOccurred())
			validation := middleware.OpenAPIValidation(swagger)
			handler = validation(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
		})

		patch := func(body string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPatch,
				"/O2ims_infrastructureMonitoring/v1/alarmSubscriptions/"+uuid.New().String(),
				bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/merge-patch+json")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			return rr
		}

		It("rejects a body containing only unknown fields with 400", func() {
			rr := patch(`{"eventFiltre":"NEW"}`)
			Expect(rr.Code).To(Equal(http.StatusBadRequest))
		})

		It("accepts a body with a known field", func() {
			rr := patch(`{"eventFilter":"NEW"}`)
			Expect(rr.Code).To(Equal(http.StatusOK))
		})
	})
})

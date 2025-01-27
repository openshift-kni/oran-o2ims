package api_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/api"
	alarmapi "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

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
					Return(nil, utils.ErrNotFound)

				resp, err := server.GetAlarm(ctx, alarmapi.GetAlarmRequestObject{
					AlarmEventRecordId: testUUID,
				})

				Expect(err).NotTo(HaveOccurred())
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

				Expect(err).NotTo(HaveOccurred())
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
})

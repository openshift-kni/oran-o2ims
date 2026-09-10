/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package serviceconfig_test

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	repogenerated "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/serviceconfig"
)

var _ = Describe("RunCleanup", func() {
	var (
		ctrl     *gomock.Controller
		mockRepo *repogenerated.MockAlarmRepositoryInterface
		cfg      *serviceconfig.Config
		ctx      context.Context
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = repogenerated.NewMockAlarmRepositoryInterface(ctrl)
		cfg = &serviceconfig.Config{Repository: mockRepo}
		ctx = context.Background()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("deletes resolved events using the retention period from the current service configuration", func() {
		mockRepo.EXPECT().GetServiceConfigurations(ctx).
			Return([]models.ServiceConfiguration{{ID: uuid.New(), RetentionPeriod: 10}}, nil)
		// The retention period read from the configuration is passed straight to the
		// delete, so a PATCH/PUT that changes it takes effect on the next run.
		mockRepo.EXPECT().DeleteResolvedAlarmEventsBefore(ctx, 10).Return(int64(3), true, nil)

		Expect(cfg.RunCleanup(ctx)).To(Succeed())
	})

	It("is a no-op (no error) when another replica holds the cleanup lock", func() {
		mockRepo.EXPECT().GetServiceConfigurations(ctx).
			Return([]models.ServiceConfiguration{{RetentionPeriod: 10}}, nil)
		mockRepo.EXPECT().DeleteResolvedAlarmEventsBefore(ctx, 10).Return(int64(0), false, nil)

		Expect(cfg.RunCleanup(ctx)).To(Succeed())
	})

	It("returns an error when reading the service configuration fails", func() {
		mockRepo.EXPECT().GetServiceConfigurations(ctx).Return(nil, errors.New("db unavailable"))

		Expect(cfg.RunCleanup(ctx)).ToNot(Succeed())
	})

	It("returns an error when there is not exactly one service configuration", func() {
		mockRepo.EXPECT().GetServiceConfigurations(ctx).Return([]models.ServiceConfiguration{}, nil)

		Expect(cfg.RunCleanup(ctx)).ToNot(Succeed())
	})

	It("returns an error when deleting resolved events fails", func() {
		mockRepo.EXPECT().GetServiceConfigurations(ctx).
			Return([]models.ServiceConfiguration{{RetentionPeriod: 10}}, nil)
		mockRepo.EXPECT().DeleteResolvedAlarmEventsBefore(ctx, 10).
			Return(int64(0), false, errors.New("delete failed"))

		Expect(cfg.RunCleanup(ctx)).ToNot(Succeed())
	})
})

var _ = Describe("Start", func() {
	var (
		ctrl     *gomock.Controller
		mockRepo *repogenerated.MockAlarmRepositoryInterface
		cfg      *serviceconfig.Config
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRepo = repogenerated.NewMockAlarmRepositoryInterface(ctrl)
		cfg = &serviceconfig.Config{Repository: mockRepo}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("runs an initial cleanup and returns once the context is cancelled", func() {
		// Cancel before Start so it performs the single initial cleanup and then
		// exits on the next loop iteration, keeping the test deterministic.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mockRepo.EXPECT().GetServiceConfigurations(gomock.Any()).
			Return([]models.ServiceConfiguration{{RetentionPeriod: 10}}, nil)
		mockRepo.EXPECT().DeleteResolvedAlarmEventsBefore(gomock.Any(), 10).
			Return(int64(1), true, nil)

		done := make(chan struct{})
		go func() {
			defer close(done)
			cfg.Start(ctx, time.Hour)
		}()

		Eventually(done, 5*time.Second).Should(BeClosed())
	})
})

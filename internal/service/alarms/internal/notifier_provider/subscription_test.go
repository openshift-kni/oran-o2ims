/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package notifier_provider_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	mockrepo "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/notifier_provider"
)

// TestGetSubscriptions_EffectiveFilter verifies that GetSubscriptions resolves the
// effective filter (event_filter precedence over filter) through the converter (C3).
func TestGetSubscriptions_EffectiveFilter(t *testing.T) {
	g := NewWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filter := api.AlarmSubscriptionInfoFilterNEW
	eventFilter := api.AlarmSubscriptionInfoEventFilterCHANGE

	repo := mockrepo.NewMockAlarmRepositoryInterface(ctrl)
	repo.EXPECT().GetAlarmSubscriptions(gomock.Any()).Return([]models.AlarmSubscription{
		// event_filter set -> precedence
		{SubscriptionID: uuid.New(), Callback: "http://a", Filter: &filter, EventFilter: &eventFilter},
		// event_filter NULL + filter set -> fallback to filter
		{SubscriptionID: uuid.New(), Callback: "http://b", Filter: &filter},
		// both NULL -> match-all (nil)
		{SubscriptionID: uuid.New(), Callback: "http://c"},
	}, nil)

	provider := notifier_provider.NewSubscriptionStorageProvider(repo)
	subs, err := provider.GetSubscriptions(context.Background())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(subs).To(HaveLen(3))

	g.Expect(subs[0].Filter).ToNot(BeNil())
	g.Expect(*subs[0].Filter).To(Equal(string(eventFilter)))

	g.Expect(subs[1].Filter).ToNot(BeNil())
	g.Expect(*subs[1].Filter).To(Equal(string(filter)))

	g.Expect(subs[2].Filter).To(BeNil())
}

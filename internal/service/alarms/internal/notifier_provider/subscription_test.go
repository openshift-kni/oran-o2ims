/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package notifier_provider_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	mockrepo "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/notifier_provider"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// TestGetSubscriptions_EffectiveFilter verifies that GetSubscriptions resolves the
// effective filter from event_filter through the converter: a set event_filter is mapped
// onto the generic Filter field and a NULL event_filter yields a nil Filter (match-all) (C3).
func TestGetSubscriptions_EffectiveFilter(t *testing.T) {
	g := NewWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	eventFilter := api.AlarmSubscriptionInfoEventFilterCHANGE

	repo := mockrepo.NewMockAlarmRepositoryInterface(ctrl)
	repo.EXPECT().GetAlarmSubscriptions(gomock.Any()).Return([]models.AlarmSubscription{
		// event_filter set -> mapped onto the generic Filter
		{SubscriptionID: uuid.New(), Callback: "http://a", EventFilter: &eventFilter},
		// event_filter NULL -> match-all (nil)
		{SubscriptionID: uuid.New(), Callback: "http://b"},
	}, nil)

	provider := notifier_provider.NewSubscriptionStorageProvider(repo)
	subs, err := provider.GetSubscriptions(context.Background())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(subs).To(HaveLen(2))

	g.Expect(subs[0].Filter).ToNot(BeNil())
	g.Expect(*subs[0].Filter).To(Equal(string(eventFilter)))

	g.Expect(subs[1].Filter).To(BeNil())
}

// TestMatches verifies notifier matching after the legacy filter removal: a NULL eventFilter
// (the effective state after a CREATE-with-null or a PATCH-to-null) matches all events, while a
// set eventFilter excludes only the event whose notificationEventType equals the filter.
func TestMatches(t *testing.T) {
	g := NewWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	provider := notifier_provider.NewSubscriptionStorageProvider(mockrepo.NewMockAlarmRepositoryInterface(ctrl))

	newEvent := &notifier.Notification{Payload: api.AlarmEventNotification{
		NotificationEventType: api.AlarmEventNotificationNotificationEventTypeNEW,
	}}

	// CREATE-with-null / PATCH-to-null => event_filter NULL => nil Filter => match-all delivery.
	matchAll := &notifier.SubscriptionInfo{}
	g.Expect(provider.Matches(matchAll, newEvent)).To(BeTrue())

	// A set eventFilter excludes the event whose type equals the filter...
	newFilter := string(api.AlarmSubscriptionInfoEventFilterNEW)
	excluded := &notifier.SubscriptionInfo{Filter: &newFilter}
	g.Expect(provider.Matches(excluded, newEvent)).To(BeFalse())

	// ...but still delivers other event types.
	ackFilter := string(api.AlarmSubscriptionInfoEventFilterACKNOWLEDGE)
	other := &notifier.SubscriptionInfo{Filter: &ackFilter}
	g.Expect(provider.Matches(other, newEvent)).To(BeTrue())
}

// TestGetSubscription verifies the by-id lookup returns the effective filter and
// preserves svcutils.ErrNotFound (C-GETSUB).
func TestGetSubscription(t *testing.T) {
	g := NewWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	id := uuid.New()
	eventFilter := api.AlarmSubscriptionInfoEventFilterCHANGE

	repo := mockrepo.NewMockAlarmRepositoryInterface(ctrl)
	repo.EXPECT().GetAlarmSubscription(gomock.Any(), id).Return(&models.AlarmSubscription{
		SubscriptionID: id, Callback: "http://a", EventFilter: &eventFilter,
	}, nil)

	provider := notifier_provider.NewSubscriptionStorageProvider(repo)
	info, err := provider.GetSubscription(context.Background(), id)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(info.SubscriptionID).To(Equal(id))
	g.Expect(info.Filter).ToNot(BeNil())
	g.Expect(*info.Filter).To(Equal(string(eventFilter)))
}

func TestGetSubscription_NotFound(t *testing.T) {
	g := NewWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	id := uuid.New()
	repo := mockrepo.NewMockAlarmRepositoryInterface(ctrl)
	repo.EXPECT().GetAlarmSubscription(gomock.Any(), id).Return(nil, svcutils.ErrNotFound)

	provider := notifier_provider.NewSubscriptionStorageProvider(repo)
	_, err := provider.GetSubscription(context.Background(), id)
	g.Expect(err).To(MatchError(svcutils.ErrNotFound))
}

// TestTransform verifies consumerSubscriptionId is serialized as a value for subscribers
// that provided one, and left as the base notification's explicit null otherwise (C4).
func TestTransform(t *testing.T) {
	g := NewWithT(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	provider := notifier_provider.NewSubscriptionStorageProvider(mockrepo.NewMockAlarmRepositoryInterface(ctrl))

	base := &notifier.Notification{Payload: api.AlarmEventNotification{
		ConsumerSubscriptionId: nullable.NewNullNullable[string](),
	}}

	// With a consumer id -> value is set on the clone.
	consumer := "consumer-123"
	out, err := provider.Transform(&notifier.SubscriptionInfo{ConsumerSubscriptionID: &consumer}, base)
	g.Expect(err).ToNot(HaveOccurred())
	payload := out.Payload.(api.AlarmEventNotification)
	g.Expect(payload.ConsumerSubscriptionId.IsNull()).To(BeFalse())
	g.Expect(payload.ConsumerSubscriptionId.MustGet()).To(Equal(consumer))

	// Without a consumer id -> the original notification (explicit null) is returned unchanged.
	out, err = provider.Transform(&notifier.SubscriptionInfo{ConsumerSubscriptionID: nil}, base)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(BeIdenticalTo(base))
}

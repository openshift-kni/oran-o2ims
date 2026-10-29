/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	. "github.com/onsi/gomega"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
)

func TestConvertAlarmEventRecordModelToApi_ObjectClass(t *testing.T) {
	g := NewWithT(t)
	objectClass := "oran.o2ims.cluster.NodeCluster"

	// Present objectClass is passed through
	record := models.ConvertAlarmEventRecordModelToApi(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
		ObjectClass:        &objectClass,
	})
	g.Expect(record.ObjectClass).ToNot(BeNil())
	g.Expect(*record.ObjectClass).To(Equal(objectClass))

	// Absent objectClass stays nil (optional field)
	recordNil := models.ConvertAlarmEventRecordModelToApi(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
	})
	g.Expect(recordNil.ObjectClass).To(BeNil())
}

func TestConvertAlarmEventRecordModelToAlarmEventNotification_ObjectClass(t *testing.T) {
	g := NewWithT(t)
	globalCloudID := uuid.New()
	objectClass := "oran.o2ims.resource.Resource"

	// Present objectClass is dereferenced into the required notification field
	notification := models.ConvertAlarmEventRecordModelToAlarmEventNotification(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
		ObjectClass:        &objectClass,
	}, globalCloudID)
	g.Expect(notification.ObjectClass).To(Equal(objectClass))

	// Absent objectClass leaves the required field as the zero value. This is a legacy-row
	// edge case that is unreachable with current code: the alertmanager converter always
	// classifies an alert as hardware or cluster (isHardwareAlert is total) and sets
	// objectClass accordingly, so object_class is never NULL for rows this code writes.
	// An empty string is accepted here rather than a sentinel because, pre-GA, there are no
	// legacy rows with a NULL object_class to migrate.
	notificationNil := models.ConvertAlarmEventRecordModelToAlarmEventNotification(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
	}, globalCloudID)
	g.Expect(notificationNil.ObjectClass).To(Equal(""))
}

// TestAlarmFilterToEventType verifies the mapping operates on the AlarmSubscriptionInfoEventFilter
// enum (decoupled from the legacy AlarmSubscriptionInfoFilter type) and maps each value to its
// corresponding notification event type.
func TestAlarmFilterToEventType(t *testing.T) {
	g := NewWithT(t)

	g.Expect(models.AlarmFilterToEventType(api.AlarmSubscriptionInfoEventFilterNEW)).
		To(Equal(api.AlarmEventNotificationNotificationEventTypeNEW))
	g.Expect(models.AlarmFilterToEventType(api.AlarmSubscriptionInfoEventFilterCHANGE)).
		To(Equal(api.AlarmEventNotificationNotificationEventTypeCHANGE))
	g.Expect(models.AlarmFilterToEventType(api.AlarmSubscriptionInfoEventFilterCLEAR)).
		To(Equal(api.AlarmEventNotificationNotificationEventTypeCLEAR))
	g.Expect(models.AlarmFilterToEventType(api.AlarmSubscriptionInfoEventFilterACKNOWLEDGE)).
		To(Equal(api.AlarmEventNotificationNotificationEventTypeACKNOWLEDGE))
}

func TestConvertSubscriptionAPIToModel_EventFilter(t *testing.T) {
	g := NewWithT(t)
	value := api.AlarmSubscriptionInfoEventFilterCHANGE

	// Omitted eventFilter -> nil model pointer (event_filter stays NULL on CREATE)
	omitted := models.ConvertSubscriptionAPIToModel(&api.AlarmSubscriptionInfo{Callback: "http://x"})
	g.Expect(omitted.EventFilter).To(BeNil())

	// Explicit null eventFilter -> nil model pointer
	explicitNull := models.ConvertSubscriptionAPIToModel(&api.AlarmSubscriptionInfo{
		Callback:    "http://x",
		EventFilter: nullable.NewNullNullable[api.AlarmSubscriptionInfoEventFilter](),
	})
	g.Expect(explicitNull.EventFilter).To(BeNil())

	// Concrete value -> stored pointer
	withValue := models.ConvertSubscriptionAPIToModel(&api.AlarmSubscriptionInfo{
		Callback:    "http://x",
		EventFilter: nullable.NewNullableWithValue(value),
	})
	g.Expect(withValue.EventFilter).ToNot(BeNil())
	g.Expect(*withValue.EventFilter).To(Equal(value))
}

func TestConvertSubscriptionModelToApi_EventFilterNullSerialization(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()
	value := api.AlarmSubscriptionInfoEventFilterCLEAR

	// NULL event_filter -> key-present explicit null (M/0..1)
	nilModel := models.ConvertSubscriptionModelToApi(models.AlarmSubscription{SubscriptionID: id, Callback: "http://x"})
	g.Expect(nilModel.EventFilter.IsSpecified()).To(BeTrue())
	g.Expect(nilModel.EventFilter.IsNull()).To(BeTrue())

	// Marshaling makes the M/0..1 contract explicit: eventFilter is emitted as an explicit
	// null (key present), not omitted.
	data, err := json.Marshal(nilModel)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring(`"eventFilter":null`))

	// Non-NULL event_filter -> value
	setModel := models.ConvertSubscriptionModelToApi(models.AlarmSubscription{SubscriptionID: id, Callback: "http://x", EventFilter: &value})
	g.Expect(setModel.EventFilter.IsSpecified()).To(BeTrue())
	g.Expect(setModel.EventFilter.IsNull()).To(BeFalse())
	got, err := setModel.EventFilter.Get()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal(value))
}

func TestConvertAlertSubToNotificationSub_EffectiveFilter(t *testing.T) {
	g := NewWithT(t)
	filter := api.AlarmSubscriptionInfoFilterNEW
	eventFilter := api.AlarmSubscriptionInfoEventFilterCHANGE

	// (a) both set -> event_filter wins (precedence)
	both := models.ConvertAlertSubToNotificationSub(&models.AlarmSubscription{Filter: &filter, EventFilter: &eventFilter})
	g.Expect(both.Filter).ToNot(BeNil())
	g.Expect(*both.Filter).To(Equal(string(eventFilter)))

	// (b) event_filter NULL + filter set -> fall back to filter
	fallback := models.ConvertAlertSubToNotificationSub(&models.AlarmSubscription{Filter: &filter})
	g.Expect(fallback.Filter).ToNot(BeNil())
	g.Expect(*fallback.Filter).To(Equal(string(filter)))

	// (c) both NULL -> nil (match-all)
	none := models.ConvertAlertSubToNotificationSub(&models.AlarmSubscription{})
	g.Expect(none.Filter).To(BeNil())
}

func TestConvertAlarmEventRecordModelToAlarmEventNotification_NullSerialization(t *testing.T) {
	g := NewWithT(t)
	globalCloudID := uuid.New()
	recordID := uuid.New()

	// All optional times absent -> fields serialize as explicit null (not omitted); objectRef
	// is built root-relative; consumerSubscriptionId is null in the base notification.
	notification := models.ConvertAlarmEventRecordModelToAlarmEventNotification(models.AlarmEventRecord{
		AlarmEventRecordID: recordID,
	}, globalCloudID)

	g.Expect(notification.AlarmAcknowledgeTime.IsSpecified()).To(BeTrue())
	g.Expect(notification.AlarmAcknowledgeTime.IsNull()).To(BeTrue())
	g.Expect(notification.AlarmChangedTime.IsNull()).To(BeTrue())
	g.Expect(notification.AlarmClearedTime.IsNull()).To(BeTrue())
	g.Expect(notification.ConsumerSubscriptionId.IsNull()).To(BeTrue())
	g.Expect(notification.ObjectRef.IsNull()).To(BeFalse())

	// objectRef is root-relative, trailing segment is the alarmEventRecordId (C5(d)).
	g.Expect(notification.ObjectRef.MustGet()).To(Equal("/O2ims_infrastructureMonitoring/v1/alarms/" + recordID.String()))

	// JSON marshaling keeps the keys present with explicit null.
	data, err := json.Marshal(notification)
	g.Expect(err).ToNot(HaveOccurred())
	js := string(data)
	g.Expect(js).To(ContainSubstring(`"alarmAcknowledgeTime":null`))
	g.Expect(js).To(ContainSubstring(`"alarmChangedTime":null`))
	g.Expect(js).To(ContainSubstring(`"alarmClearedTime":null`))
	g.Expect(js).To(ContainSubstring(`"consumerSubscriptionId":null`))
}

func TestConvertAlarmEventRecordModelToAlarmEventNotification_TimeValues(t *testing.T) {
	g := NewWithT(t)
	globalCloudID := uuid.New()
	changed := time.Date(2042, 7, 21, 21, 32, 28, 0, time.UTC)
	cleared := time.Date(2042, 7, 21, 20, 32, 28, 0, time.UTC)
	acked := time.Date(2042, 7, 21, 22, 32, 28, 0, time.UTC)

	notification := models.ConvertAlarmEventRecordModelToAlarmEventNotification(models.AlarmEventRecord{
		AlarmEventRecordID:    uuid.New(),
		AlarmChangedTime:      &changed,
		AlarmClearedTime:      &cleared,
		AlarmAcknowledgedTime: &acked,
	}, globalCloudID)

	g.Expect(notification.AlarmChangedTime.MustGet()).To(Equal(changed))
	g.Expect(notification.AlarmClearedTime.MustGet()).To(Equal(cleared))
	g.Expect(notification.AlarmAcknowledgeTime.MustGet()).To(Equal(acked))
}

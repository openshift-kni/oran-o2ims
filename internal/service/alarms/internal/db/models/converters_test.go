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

	// objectClass is a mandatory, non-pointer string on both the model and the API record;
	// it is passed through directly.
	record := models.ConvertAlarmEventRecordModelToApi(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
		ObjectClass:        objectClass,
	})
	g.Expect(record.ObjectClass).To(Equal(objectClass))
}

func TestConvertAlarmEventRecordModelToAlarmEventNotification_ObjectClass(t *testing.T) {
	g := NewWithT(t)
	globalCloudID := uuid.New()
	objectClass := "oran.o2ims.resource.Resource"

	// objectClass is mapped into the required notification field. objectId may be absent
	// (zero value) while objectClass still identifies the source class, so cover that case.
	notification := models.ConvertAlarmEventRecordModelToAlarmEventNotification(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
		ObjectClass:        objectClass,
	}, globalCloudID)
	g.Expect(notification.ObjectClass).To(Equal(objectClass))
	g.Expect(notification.ObjectId).To(Equal(uuid.Nil))
}

// TestConvertAlarmEventRecordModelToApi_V13JSONNames verifies the alarm record serializes the
// v13 JSON keys and no longer emits the pre-v13 names.
func TestConvertAlarmEventRecordModelToApi_V13JSONNames(t *testing.T) {
	g := NewWithT(t)
	defID, causeID, objID, objTypeID := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	record := models.ConvertAlarmEventRecordModelToApi(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
		AlarmDefinitionID:  &defID,
		ProbableCauseID:    &causeID,
		ObjectID:           &objID,
		ObjectTypeID:       &objTypeID,
		ObjectClass:        "oran.o2ims.cluster.NodeCluster",
	})

	data, err := json.Marshal(record)
	g.Expect(err).ToNot(HaveOccurred())
	js := string(data)

	for _, key := range []string{`"objectTypeId"`, `"objectId"`, `"alarmDefinitionId"`, `"probableCauseId"`, `"objectClass"`} {
		g.Expect(js).To(ContainSubstring(key))
	}
	for _, key := range []string{`"resourceTypeID"`, `"resourceID"`, `"alarmDefinitionID"`, `"probableCauseID"`, `"alarmAcknowledgedTime"`} {
		g.Expect(js).ToNot(ContainSubstring(key))
	}
}

// TestConvertAlarmEventNotification_V13JSONNames verifies the delivered notification serializes
// the v13 JSON keys (including globalCloudId) and no longer emits the pre-v13 names.
func TestConvertAlarmEventNotification_V13JSONNames(t *testing.T) {
	g := NewWithT(t)
	globalCloudID := uuid.New()
	defID, causeID, objID, objTypeID := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	notification := models.ConvertAlarmEventRecordModelToAlarmEventNotification(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
		AlarmDefinitionID:  &defID,
		ProbableCauseID:    &causeID,
		ObjectID:           &objID,
		ObjectTypeID:       &objTypeID,
		ObjectClass:        "oran.o2ims.resource.Resource",
	}, globalCloudID)

	g.Expect(notification.GlobalCloudId).To(Equal(globalCloudID))

	data, err := json.Marshal(notification)
	g.Expect(err).ToNot(HaveOccurred())
	js := string(data)

	for _, key := range []string{`"globalCloudId"`, `"objectTypeId"`, `"objectId"`, `"alarmDefinitionId"`, `"probableCauseId"`, `"objectClass"`, `"alarmAcknowledgeTime"`} {
		g.Expect(js).To(ContainSubstring(key))
	}
	for _, key := range []string{`"globalCloudID"`, `"resourceTypeID"`, `"resourceID"`, `"alarmDefinitionID"`, `"probableCauseID"`, `"alarmAcknowledgedTime"`} {
		g.Expect(js).ToNot(ContainSubstring(key))
	}
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
	eventFilter := api.AlarmSubscriptionInfoEventFilterCHANGE

	// (a) event_filter set -> mapped onto the generic Filter field
	set := models.ConvertAlertSubToNotificationSub(&models.AlarmSubscription{EventFilter: &eventFilter})
	g.Expect(set.Filter).ToNot(BeNil())
	g.Expect(*set.Filter).To(Equal(string(eventFilter)))

	// (b) event_filter NULL -> nil (match-all); the legacy filter fallback has been removed
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

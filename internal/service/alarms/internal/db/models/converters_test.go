/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models_test

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"

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

	// Absent objectClass leaves the required field as the zero value
	notificationNil := models.ConvertAlarmEventRecordModelToAlarmEventNotification(models.AlarmEventRecord{
		AlarmEventRecordID: uuid.New(),
	}, globalCloudID)
	g.Expect(notificationNil.ObjectClass).To(Equal(""))
}

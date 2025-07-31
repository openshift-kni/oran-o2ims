/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package service

import "github.com/openshift-kni/oran-o2ims/internal/constants"

const (
	DefaultAlarmConfigmapName          = constants.DefaultNamespace + "-alarm-subscriptions"
	DefaultInfraInventoryConfigmapName = constants.DefaultNamespace + "-inventory-subscriptions"
	FieldOwner                         = constants.DefaultNamespace
)

const (
	SubscriptionIdAlarm                   = "alarmSubscriptionId"
	SubscriptionIdInfrastructureInventory = "subscriptionId"
)

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package service

import "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"

const (
	// default namespace should be changed to official namespace when it is available
	DefaultNamespace                   = utils.DefaultNamespace // TODO: consolidate
	DefaultAlarmConfigmapName          = "oran-o2ims-alarm-subscriptions"
	DefaultInfraInventoryConfigmapName = "oran-o2ims-inventory-subscriptions"
	FieldOwner                         = "oran-o2ims"
)

const (
	SubscriptionIdAlarm                   = "alarmSubscriptionId"
	SubscriptionIdInfrastructureInventory = "subscriptionId"
)

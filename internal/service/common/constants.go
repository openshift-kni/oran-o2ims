/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package common

// Hardware monitoring alert label constants (group-level labels)
const (
	// HardwareAlertTypeLabel is the label key used to identify hardware monitoring alert groups
	HardwareAlertTypeLabel = "type"
	// HardwareAlertTypeValue is the label value for hardware monitoring alert groups
	HardwareAlertTypeValue = "hardware"
	// HardwareAlertComponentLabel is the label key used to identify the hardware component
	HardwareAlertComponentLabel = "component"
	// HardwareAlertComponentValue is the label value for ironic hardware monitoring
	HardwareAlertComponentValue = "ironic"
)

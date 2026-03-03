/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"testing"
)

func TestGetHardwarePluginRef(t *testing.T) {
	tests := []struct {
		name     string
		spec     HardwareTemplateSpec
		expected string
	}{
		{
			name:     "returns default when empty",
			spec:     HardwareTemplateSpec{},
			expected: DefaultHardwarePluginRef,
		},
		{
			name: "returns explicit value when set",
			spec: HardwareTemplateSpec{
				HardwarePluginRef: "custom-hwplugin",
			},
			expected: "custom-hwplugin",
		},
		{
			name: "returns explicit value even if it matches default",
			spec: HardwareTemplateSpec{
				HardwarePluginRef: DefaultHardwarePluginRef,
			},
			expected: DefaultHardwarePluginRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.GetHardwarePluginRef()
			if got != tt.expected {
				t.Errorf("GetHardwarePluginRef() = %q, want %q", got, tt.expected)
			}
		})
	}
}

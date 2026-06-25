/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metrics

import (
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "collection endpoint unchanged",
			input:    "/o2ims-infrastructureInventory/v1/resourcePools",
			expected: "/o2ims-infrastructureInventory/v1/resourcePools",
		},
		{
			name:     "single UUID replaced",
			input:    "/o2ims-infrastructureInventory/v1/resourcePools/bf3f8f2e-6f37-4882-96ad-c0b9cef6fc04",
			expected: "/o2ims-infrastructureInventory/v1/resourcePools/{id}",
		},
		{
			name:     "nested resource with two UUIDs",
			input:    "/o2ims-infrastructureInventory/v1/resourcePools/bf3f8f2e-6f37-4882-96ad-c0b9cef6fc04/resources/a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			expected: "/o2ims-infrastructureInventory/v1/resourcePools/{id}/resources/{id}",
		},
		{
			name:     "uppercase UUID replaced",
			input:    "/o2ims-infrastructureCluster/v1/nodeClusters/BF3F8F2E-6F37-4882-96AD-C0B9CEF6FC04",
			expected: "/o2ims-infrastructureCluster/v1/nodeClusters/{id}",
		},
		{
			name:     "path without UUIDs unchanged",
			input:    "/o2ims-infrastructureMonitoring/v1/alarms",
			expected: "/o2ims-infrastructureMonitoring/v1/alarms",
		},
		{
			name:     "empty path unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "root path unchanged",
			input:    "/",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePath(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

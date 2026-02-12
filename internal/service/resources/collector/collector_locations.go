/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"fmt"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// listLocations retrieves all Location CRs from the Kubernetes API.
// This function only performs the K8s read operation and returns the raw CRD objects.
func (c *Collector) listLocations(ctx context.Context) ([]inventoryv1alpha1.Location, error) {
	if c.hubClient == nil {
		return nil, fmt.Errorf("hubClient is not initialized")
	}

	var locationList inventoryv1alpha1.LocationList
	if err := c.hubClient.List(ctx, &locationList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("failed to list Location CRs: %w", err)
	}

	return locationList.Items, nil
}

// listOCloudSites retrieves all OCloudSite CRs from the Kubernetes API.
// This function only performs the K8s read operation and returns the raw CRD objects.
func (c *Collector) listOCloudSites(ctx context.Context) ([]inventoryv1alpha1.OCloudSite, error) {
	if c.hubClient == nil {
		return nil, fmt.Errorf("hubClient is not initialized")
	}

	var siteList inventoryv1alpha1.OCloudSiteList
	if err := c.hubClient.List(ctx, &siteList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("failed to list OCloudSite CRs: %w", err)
	}

	return siteList.Items, nil
}

// NewCollectorForTest creates a Collector with only hubClient for testing list functions.
// This is used by envtest tests that only need to test K8s CR reading.
func NewCollectorForTest(hubClient client.Client) *Collector {
	return &Collector{
		hubClient: hubClient,
	}
}

// ListLocations exposes listLocations for testing.
func (c *Collector) ListLocations(ctx context.Context) ([]inventoryv1alpha1.Location, error) {
	return c.listLocations(ctx)
}

// ListOCloudSites exposes listOCloudSites for testing.
func (c *Collector) ListOCloudSites(ctx context.Context) ([]inventoryv1alpha1.OCloudSite, error) {
	return c.listOCloudSites(ctx)
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"

	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Index field paths for hierarchy CRs.
const (
	// OCloudSiteGlobalLocationNameIndex indexes OCloudSite by spec.globalLocationName.
	// Used by Location controller to find dependent OCloudSites.
	OCloudSiteGlobalLocationNameIndex = "spec.globalLocationName"

	// ResourcePoolOCloudSiteNameIndex indexes ResourcePool by spec.oCloudSiteName.
	// Used by OCloudSite controller to find dependent ResourcePools.
	ResourcePoolOCloudSiteNameIndex = "spec.oCloudSiteName"
)

// ParentValidationResult holds the result of validating a parent CR reference.
// Used by hierarchy controllers (OCloudSite, ResourcePool) to check if their
// parent resource exists and is ready before marking themselves as ready.
type ParentValidationResult struct {
	Exists bool // Whether the parent resource exists
	Ready  bool // Whether the parent resource has Ready=True condition
}

// registerIndex is a generic helper to register a field index for a CR type.
// T is the concrete type (e.g., Location), PT is the pointer type (*Location) that implements client.Object.
func registerIndex[T any, PT interface {
	*T
	client.Object
}](
	ctx context.Context,
	indexer client.FieldIndexer,
	indexName string,
	getField func(PT) string,
) error {
	var zero T
	if err := indexer.IndexField(ctx, PT(&zero), indexName,
		func(obj client.Object) []string {
			if val := getField(obj.(PT)); val != "" {
				return []string{val}
			}
			return nil
		}); err != nil {
		return fmt.Errorf("failed to register index %q: %w", indexName, err)
	}
	return nil
}

// SetupHierarchyIndexers registers field indexes for hierarchy CRs.
func SetupHierarchyIndexers(ctx context.Context, mgr ctrl.Manager) error {
	indexer := mgr.GetFieldIndexer()

	// OCloudSite: index by spec.globalLocationName (for Location to find dependent OCloudSites)
	if err := registerIndex(ctx, indexer, OCloudSiteGlobalLocationNameIndex,
		func(s *inventoryv1alpha1.OCloudSite) string { return s.Spec.GlobalLocationName },
	); err != nil {
		return fmt.Errorf("failed to setup OCloudSite globalLocationName index: %w", err)
	}

	// ResourcePool: index by spec.oCloudSiteName (for OCloudSite to find dependent ResourcePools)
	if err := registerIndex(ctx, indexer, ResourcePoolOCloudSiteNameIndex,
		func(p *inventoryv1alpha1.ResourcePool) string { return p.Spec.OCloudSiteName },
	); err != nil {
		return fmt.Errorf("failed to setup ResourcePool oCloudSiteName index: %w", err)
	}

	return nil
}

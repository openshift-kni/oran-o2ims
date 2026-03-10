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
// These constants define the field paths used for indexed queries.
const (
	// Shared index for Location and OCloudSite (both use spec.globalLocationId)
	GlobalLocationIDIndex = "spec.globalLocationId"

	// OCloudSite
	OCloudSiteSiteIDIndex = "spec.siteId"

	// ResourcePool
	ResourcePoolOCloudSiteIDIndex   = "spec.oCloudSiteId"
	ResourcePoolResourcePoolIDIndex = "spec.resourcePoolId"
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
	return indexer.IndexField(ctx, PT(&zero), indexName,
		func(obj client.Object) []string {
			if val := getField(obj.(PT)); val != "" {
				return []string{val}
			}
			return nil
		})
}

// SetupHierarchyIndexers registers field indexes for hierarchy CRs (Location, OCloudSite, ResourcePool).
func SetupHierarchyIndexers(ctx context.Context, mgr ctrl.Manager) error {
	indexer := mgr.GetFieldIndexer()

	// Location: index by globalLocationId (for OCloudSite parent lookup)
	if err := registerIndex(ctx, indexer, GlobalLocationIDIndex,
		func(l *inventoryv1alpha1.Location) string { return l.Spec.GlobalLocationID },
	); err != nil {
		return fmt.Errorf("failed to setup Location index: %w", err)
	}

	// OCloudSite: index by globalLocationId (for finding sites that reference a Location)
	if err := registerIndex(ctx, indexer, GlobalLocationIDIndex,
		func(s *inventoryv1alpha1.OCloudSite) string { return s.Spec.GlobalLocationID },
	); err != nil {
		return fmt.Errorf("failed to setup OCloudSite globalLocationId index: %w", err)
	}

	// OCloudSite: index by siteId (for ResourcePool parent lookup)
	if err := registerIndex(ctx, indexer, OCloudSiteSiteIDIndex,
		func(s *inventoryv1alpha1.OCloudSite) string { return s.Spec.SiteID },
	); err != nil {
		return fmt.Errorf("failed to setup OCloudSite siteId index: %w", err)
	}

	// ResourcePool: index by oCloudSiteId (for finding pools that reference an OCloudSite)
	if err := registerIndex(ctx, indexer, ResourcePoolOCloudSiteIDIndex,
		func(p *inventoryv1alpha1.ResourcePool) string { return p.Spec.OCloudSiteId },
	); err != nil {
		return fmt.Errorf("failed to setup ResourcePool oCloudSiteId index: %w", err)
	}

	// ResourcePool: index by resourcePoolId (for potential webhook optimization)
	if err := registerIndex(ctx, indexer, ResourcePoolResourcePoolIDIndex,
		func(p *inventoryv1alpha1.ResourcePool) string { return p.Spec.ResourcePoolId },
	); err != nil {
		return fmt.Errorf("failed to setup ResourcePool resourcePoolId index: %w", err)
	}

	return nil
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/inventory"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// GetCurrentResources parses the nodelist configmap to get the current available and allocated resource lists
func GetCurrentResources(ctx context.Context, c client.Client, logger *slog.Logger, loopbackNamespace string) (
	cm *corev1.ConfigMap, resources cmResources, allocations cmAllocations, err error) {
	cm, err = utils.GetConfigmap(ctx, c, cmName, loopbackNamespace)
	if err != nil {
		err = fmt.Errorf("unable to get configmap: %w", err)
		return
	}

	resources, err = utils.ExtractDataFromConfigMap[cmResources](cm, resourcesKey)
	if err != nil {
		err = fmt.Errorf("unable to parse resources from configmap: %w", err)
		return
	}

	allocations, err = utils.ExtractDataFromConfigMap[cmAllocations](cm, allocationsKey)
	if err != nil {
		// Allocated node field may not be present
		logger.InfoContext(ctx, "unable to parse allocations from configmap")
		err = nil
	}

	return
}

func GetResourcePools(ctx context.Context, c client.Client, logger *slog.Logger, loopbackNamespace string) (inventory.GetResourcePoolsResponseObject, error) {
	var resp []inventory.ResourcePoolInfo
	_, resources, _, err := GetCurrentResources(ctx, c, logger, loopbackNamespace)
	if err != nil {
		return nil, fmt.Errorf("unable to get current resources: %w", err)
	}

	siteId := "n/a"
	for _, pool := range resources.ResourcePools {
		resp = append(resp, inventory.ResourcePoolInfo{
			ResourcePoolId: pool,
			Description:    pool,
			Name:           pool,
			SiteId:         &siteId,
		})
	}

	return inventory.GetResourcePools200JSONResponse(resp), nil
}

func convertProcessorInfo(infos []processorInfo) []inventory.ProcessorInfo {
	result := make([]inventory.ProcessorInfo, len(infos))
	for i, info := range infos {
		result[i] = inventory.ProcessorInfo{
			Architecture: &info.Architecture,
			Cores:        &info.Cores,
			Model:        &info.Model,
			Manufacturer: &info.Manufacturer,
		}
	}
	return result
}

func GetResources(ctx context.Context, c client.Client, logger *slog.Logger, loopbackNamespace string) (inventory.GetResourcesResponseObject, error) {
	var resp []inventory.ResourceInfo

	_, resources, _, err := GetCurrentResources(ctx, c, logger, loopbackNamespace)
	if err != nil {
		return nil, fmt.Errorf("unable to get current resources: %w", err)
	}

	for name, server := range resources.Nodes {
		powerState := inventory.ResourceInfoPowerState("ON")
		resp = append(resp, inventory.ResourceInfo{
			AdminState:       inventory.ResourceInfoAdminState(server.AdminState),
			Description:      server.Description,
			GlobalAssetId:    &server.GlobalAssetID,
			Groups:           nil,
			HwProfile:        "loopback-profile",
			Labels:           &server.Labels,
			Memory:           server.Memory,
			Model:            server.Model,
			Name:             name,
			OperationalState: inventory.ResourceInfoOperationalState(server.OperationalState),
			PartNumber:       server.PartNumber,
			PowerState:       &powerState,
			Processors:       convertProcessorInfo(server.Processors),
			ResourceId:       name,
			ResourcePoolId:   server.ResourcePoolID,
			SerialNumber:     server.SerialNumber,
			Tags:             nil,
			UsageState:       inventory.ResourceInfoUsageState(server.UsageState),
			Vendor:           server.Vendor,
		})
	}
	return inventory.GetResources200JSONResponse(resp), nil
}

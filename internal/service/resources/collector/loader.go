/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

type HwPluginDataSourceLoader struct {
	cloudID       uuid.UUID
	globalCloudID uuid.UUID
	hubClient     client.WithWatch
}

func NewHwPluginDataSourceLoader(cloudID, globalCloudID uuid.UUID) (DataSourceLoader, error) {
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	return &HwPluginDataSourceLoader{
		cloudID:       cloudID,
		globalCloudID: globalCloudID,
		hubClient:     hubClient,
	}, nil
}

func (l *HwPluginDataSourceLoader) Load(ctx context.Context) ([]DataSource, error) {
	slog.Info("Loading hardware plugin data sources")

	var hwPlugins hwmgmtv1alpha1.HardwarePluginList
	err := l.hubClient.List(ctx, &hwPlugins)
	if err != nil {
		return nil, fmt.Errorf("failed to list hardware plugins: %w", err)
	}

	var result []DataSource
	for _, hwPlugin := range hwPlugins.Items {
		// TODO: check connectivity status

		ds, err := NewHwPluginDataSource(ctx, l.hubClient, &hwPlugin, l.cloudID, l.globalCloudID)
		if err != nil {
			return nil, fmt.Errorf("failed to load HardwarePlugin '%s' data sources: %w", hwPlugin.Name, err)
		}
		result = append(result, ds)
	}

	return result, nil
}

package collector

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	hwmgrv1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

type HwMgrDataSourceLoader struct {
	cloudID       uuid.UUID
	globalCloudID uuid.UUID
	hubClient     client.WithWatch
}

func NewHwMgrDataSourceLoader(cloudID, globalCloudID uuid.UUID) (DataSourceLoader, error) {
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	return &HwMgrDataSourceLoader{
		cloudID:       cloudID,
		globalCloudID: globalCloudID,
		hubClient:     hubClient,
	}, nil
}

func (l *HwMgrDataSourceLoader) Load(ctx context.Context) ([]DataSource, error) {
	slog.Info("Loading hardware manager data sources")

	var hwMgrs hwmgrv1.HardwareManagerList
	err := l.hubClient.List(ctx, &hwMgrs)
	if err != nil {
		return nil, fmt.Errorf("failed to list hardware managers: %w", err)
	}

	var result []DataSource
	for _, hwMgr := range hwMgrs.Items {
		// TODO: check connectivity status

		ds, err := NewHwMgrDataSource(hwMgr.Name, l.cloudID, l.globalCloudID)
		if err != nil {
			return nil, fmt.Errorf("failed to load hardware manager data sources: %w", err)
		}
		result = append(result, ds)
	}

	return result, nil
}

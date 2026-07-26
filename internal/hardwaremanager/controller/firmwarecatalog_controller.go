/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

func validateCatalogImages(images []hwmgmtv1alpha1.FirmwareImage) []hwmgmtv1alpha1.ImageValidationStatus {
	statuses := make([]hwmgmtv1alpha1.ImageValidationStatus, 0, len(images))
	for _, img := range images {
		status := hwmgmtv1alpha1.ImageValidationStatus{
			Name:  img.Name,
			Valid: true,
		}

		if !ctlrutils.IsValidURL(img.URL) {
			status.Valid = false
			status.Reason = "InvalidURL"
			status.Message = fmt.Sprintf("URL %q is not a valid HTTP(S) URL", img.URL)
		}

		statuses = append(statuses, status)
	}
	return statuses
}

// EnsureFirmwareCatalogSingleton creates the singleton FirmwareCatalog CR if it
// does not already exist. It never overwrites user content.
// This is safe to call before mgr.Start() because Create bypasses the cache.
func EnsureFirmwareCatalogSingleton(ctx context.Context, c client.Client, namespace string) error {
	catalog := &hwmgmtv1alpha1.FirmwareCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hwmgmtv1alpha1.FirmwareCatalogName,
			Namespace: namespace,
		},
		Spec: hwmgmtv1alpha1.FirmwareCatalogSpec{},
	}

	if err := c.Create(ctx, catalog); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create FirmwareCatalog singleton: %w", err)
	}

	return nil
}

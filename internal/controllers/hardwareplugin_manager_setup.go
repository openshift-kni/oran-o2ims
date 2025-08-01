/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// setupHardwarePluginManager creates the Kubernetes resources necessary to start the Hadware Plugin manager.
func (t *reconcilerTask) setupHardwarePluginManager(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {
	nextReconcile = defaultResult

	if err = t.createServiceAccount(ctx, ctlrutils.HardwarePluginManagerServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy ServiceAccount for the HardwarePlugin manager.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createHardwarePluginManagerClusterRole(ctx); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create ClusterRole for the HardwarePlugin manager.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerClusterRoleBinding(ctx, ctlrutils.HardwarePluginManagerServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create server ClusterRoleBinding for the HardwarePlugin manager.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerRbacClusterRoleBinding(ctx, ctlrutils.HardwarePluginManagerServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create RBAC ClusterRoleBinding for the HardwarePlugin manager.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createService(ctx, ctlrutils.HardwarePluginManagerServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy Service for the HardwarePlugin manager.",
			slog.String("error", err.Error()))
		return
	}

	errorReason, err := t.deployServer(ctx, ctlrutils.HardwarePluginManagerServerName)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy the HardwarePlugin manager.",
			slog.String("error", err.Error()))
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
		}
	}

	return nextReconcile, err
}

func (t *reconcilerTask) createHardwarePluginManagerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, ctlrutils.HardwarePluginManagerServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			// Namespaces
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"namespaces",
				},
				Verbs: []string{
					"create",
					"get",
					"list",
					"patch",
					"update",
					"watch",
				},
			},
			// ConfigMaps
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"configmaps",
				},
				Verbs: []string{
					"create",
					"get",
					"list",
					"patch",
					"update",
					"watch",
				},
			},
			// Secrets
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"secrets",
				},
				Verbs: []string{
					"create",
					"get",
					"list",
					"patch",
					"update",
					"watch",
				},
			},
			// HardwarePlugins
			{
				APIGroups: []string{
					"clcm.openshift.io",
				},
				Resources: []string{
					"hardwareplugins",
				},
				Verbs: []string{
					"create",
					"get",
					"list",
					"patch",
					"update",
					"watch",
				},
			},
			{
				APIGroups: []string{
					"clcm.openshift.io",
				},
				Resources: []string{
					"hardwareplugins/status",
				},
				Verbs: []string{
					"get",
					"update",
					"patch",
				},
			},
			{
				APIGroups: []string{
					"clcm.openshift.io",
				},
				Resources: []string{
					"hardwareplugins/finalizers",
				},
				Verbs: []string{
					"update",
					"patch",
				},
			},
			// Leases
			{
				APIGroups: []string{
					"coordination.k8s.io",
				},
				Resources: []string{
					"leases",
				},
				Verbs: []string{
					"create",
					"get",
					"list",
					"patch",
					"update",
					"watch",
					"delete",
				},
			},
			// Events
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"events",
				},
				Verbs: []string{
					"create",
					"patch",
					"update",
				},
			},
			// HardwarePlugin registration verification
			{
				NonResourceURLs: []string{
					constants.HardwareManagerProvisioningAPIPath + "/*",
				},
				Verbs: []string{
					"get",
				},
			},
		},
	}

	if err := ctlrutils.CreateK8sCR(ctx, t.client, role, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Cluster Server cluster role: %w", err)
	}

	return nil
}

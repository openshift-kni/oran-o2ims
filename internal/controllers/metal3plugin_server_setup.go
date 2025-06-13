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

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// setupMetal3PluginServer creates the Kubernetes resources necessary to start the metal3 hardware plugin server.
func (t *reconcilerTask) setupMetal3PluginServer(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {

	nextReconcile = defaultResult

	if err = t.createServiceAccount(ctx, utils.Metal3PluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy ServiceAccount for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createMetal3PluginServerClusterRole(ctx); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create ClusterRole for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerClusterRoleBinding(ctx, utils.Metal3PluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create server ClusterRoleBinding for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerRbacClusterRoleBinding(ctx, utils.Metal3PluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create RBAC ClusterRoleBinding for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createService(ctx, utils.Metal3PluginServerName, utils.DefaultServicePort, utils.DefaultServiceTargetPort); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy Service for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	errorReason, err := t.deployServer(ctx, utils.Metal3PluginServerName)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
		}
	}

	return nextReconcile, err
}

func (t *reconcilerTask) createMetal3PluginServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.Metal3PluginServerName,
			),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"namespaces",
					"secrets",
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
			{
				APIGroups: []string{
					"o2ims-hardwaremanagement.oran.openshift.io",
				},
				Resources: []string{
					"nodeallocationrequests",
					"allocatednodes",
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
					"o2ims-hardwaremanagement.oran.openshift.io",
				},
				Resources: []string{
					"nodeallocationrequests/status",
					"allocatednodes/status",
				},
				Verbs: []string{
					"get",
					"patch",
					"update",
				},
			},
			{
				APIGroups: []string{
					"o2ims-hardwaremanagement.oran.openshift.io",
				},
				Resources: []string{
					"nodeallocationrequests/finalizers",
					"allocatednodes/finalizers",
				},
				Verbs: []string{
					"update",
				},
			},
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
			// Provisioning URL
			{
				NonResourceURLs: []string{
					"/hardware-manager/provisioning/*",
				},
				Verbs: []string{
					"get",
					"list",
					"create",
					"post",
					"put",
					"delete",
				},
			},
		},
	}

	if err := utils.CreateK8sCR(ctx, t.client, role, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Cluster Server cluster role: %w", err)
	}

	return nil
}

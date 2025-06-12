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

// setupLoopbackPluginServer creates the Kubernetes resources necessary to start the loopback hardware plugin server.
func (t *reconcilerTask) setupLoopbackPluginServer(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {

	nextReconcile = defaultResult

	if err = t.createServiceAccount(ctx, utils.LoopbackPluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy ServiceAccount for the Loopback hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createLoopbackPluginServerClusterRole(ctx); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create ClusterRole for the Loopback hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerClusterRoleBinding(ctx, utils.LoopbackPluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create server ClusterRoleBinding for the Loopback hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerRbacClusterRoleBinding(ctx, utils.LoopbackPluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create RBAC ClusterRoleBinding for the Loopback hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createService(ctx, utils.LoopbackPluginServerName, utils.DefaultServicePort, utils.DefaultServiceTargetPort); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy Service for the Loopback hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	errorReason, err := t.deployServer(ctx, utils.LoopbackPluginServerName)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy the Loopback plugin hardware server.",
			slog.String("error", err.Error()))
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
		}
	}

	return nextReconcile, err
}

func (t *reconcilerTask) createLoopbackPluginServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, utils.LoopbackPluginServerName,
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
					"delete",
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

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

	"github.com/openshift-kni/oran-o2ims/api/common"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// setupMetal3PluginServer creates the Kubernetes resources necessary to start the metal3 hardware plugin server.
func (t *reconcilerTask) setupMetal3PluginServer(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {

	nextReconcile = defaultResult

	if err = t.createServiceAccount(ctx, ctlrutils.Metal3PluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy ServiceAccount for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createMetal3PluginServerClusterRole(ctx); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create ClusterRole for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerClusterRoleBinding(ctx, ctlrutils.Metal3PluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create server ClusterRoleBinding for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerRbacClusterRoleBinding(ctx, ctlrutils.Metal3PluginServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create RBAC ClusterRoleBinding for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createService(ctx, ctlrutils.Metal3PluginServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy Service for the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		return
	}

	errorReason, err := t.deployServer(ctx, ctlrutils.Metal3PluginServerName)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy the Metal3 hardware plugin server.",
			slog.String("error", err.Error()))
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
		}
	}

	if err = t.createMetal3PluginHardwarePluginCR(ctx); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create Metal3 Hardware Plugin CR.",
			slog.String("error", err.Error()))
		return
	}

	return nextReconcile, err
}

func (t *reconcilerTask) createMetal3PluginServerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, ctlrutils.Metal3PluginServerName,
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
					"clcm.openshift.io",
				},
				Resources: []string{
					"hardwareprofiles",
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
					"hardwareprofiles/status",
				},
				Verbs: []string{
					"get",
					"patch",
					"update",
				},
			},
			{
				APIGroups: []string{
					"plugins.clcm.openshift.io",
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
					"plugins.clcm.openshift.io",
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
					"plugins.clcm.openshift.io",
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
			// Metal3
			{
				APIGroups: []string{
					"metal3.io",
				},
				Resources: []string{
					"baremetalhosts",
					"preprovisioningimages",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"update",
					"patch",
				},
			},
			{
				APIGroups: []string{
					"metal3.io",
				},
				Resources: []string{
					"hostfirmwaresettings",
					"hostfirmwarecomponents",
					"hostupdatepolicies",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"create",
					"update",
					"patch",
				},
			},
			{
				APIGroups: []string{
					"metal3.io",
				},
				Resources: []string{
					"firmwareschemas",
					"hardwaredata",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
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
					"update",
					"delete",
				},
			},
			// Nar Callback
			{
				NonResourceURLs: []string{
					"/nar-callback/*",
				},
				Verbs: []string{
					"get",
					"list",
					"create",
					"update",
					"delete",
				},
			},
		},
	}

	if err := ctlrutils.CreateK8sCR(ctx, t.client, role, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Cluster Server cluster role: %w", err)
	}

	return nil
}

// createMetal3PluginHardwarePluginCR creates the Metal3 Hardware Plugin CR
func (t *reconcilerTask) createMetal3PluginHardwarePluginCR(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Creating Metal3 Hardware Plugin CR")
	hardwarePlugin := &hwmgmtv1alpha1.HardwarePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hwmgrutils.Metal3HardwarePluginID,
			Namespace: t.object.Namespace,
		},
		Spec: hwmgmtv1alpha1.HardwarePluginSpec{
			ApiRoot: fmt.Sprintf("https://%s.%s.svc.cluster.local:8443", ctlrutils.Metal3PluginServerName, t.object.Namespace),
			AuthClientConfig: &common.AuthClientConfig{
				Type: common.ServiceAccount,
			},
		},
	}

	if err := ctlrutils.CreateK8sCR(ctx, t.client, hardwarePlugin, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Metal3 Hardware Plugin CR: %w", err)
	}

	return nil
}

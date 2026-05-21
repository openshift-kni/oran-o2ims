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

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// API group constants used in RBAC PolicyRules across this package.
const (
	apiGroupCLCM   = "clcm.openshift.io"
	apiGroupMetal3 = "metal3.io"
)

// setupHardwareManager creates the Kubernetes resources necessary to start the hardware manager.
func (t *reconcilerTask) setupHardwareManager(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {

	nextReconcile = defaultResult

	if err = t.createServiceAccount(ctx, ctlrutils.HardwareManagerServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy ServiceAccount for the hardware manager server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createHardwareManagerClusterRole(ctx); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create ClusterRole for the hardware manager server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerClusterRoleBinding(ctx, ctlrutils.HardwareManagerServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create server ClusterRoleBinding for the hardware manager server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createServerRbacClusterRoleBinding(ctx, ctlrutils.HardwareManagerServerName); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create RBAC ClusterRoleBinding for the hardware manager server.",
			slog.String("error", err.Error()))
		return
	}

	if err = t.createService(ctx, ctlrutils.HardwareManagerServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy Service for the hardware manager server.",
			slog.String("error", err.Error()))
		return
	}

	errorReason, err := t.deployServer(ctx, ctlrutils.HardwareManagerServerName)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy the hardware manager server.",
			slog.String("error", err.Error()))
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
		}
	}

	return nextReconcile, err
}

func (t *reconcilerTask) createHardwareManagerClusterRole(ctx context.Context) error {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(
				"%s-%s", t.object.Namespace, ctlrutils.HardwareManagerServerName,
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
					apiGroupCLCM,
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
					apiGroupCLCM,
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
			// ProvisioningRequest read access for node hostname mapping during Day2 updates
			{
				APIGroups: []string{
					apiGroupCLCM,
				},
				Resources: []string{
					"provisioningrequests",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
			{
				APIGroups: []string{
					apiGroupCLCM,
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
					apiGroupCLCM,
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
					apiGroupCLCM,
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
					apiGroupMetal3,
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
					apiGroupMetal3,
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
					apiGroupMetal3,
				},
				Resources: []string{
					"dataimages",
				},
				Verbs: []string{
					"get",
					"list",
					"delete",
				},
			},
			{
				APIGroups: []string{
					apiGroupMetal3,
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
			// ResourcePools: needed to lookup pool name -> UID mapping for inventory API
			{
				APIGroups: []string{
					"ocloud.openshift.io",
				},
				Resources: []string{
					"resourcepools",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
			},
		},
	}

	if err := ctlrutils.CreateK8sCR(ctx, t.client, role, t.object, ctlrutils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Cluster Server cluster role: %w", err)
	}

	return nil
}

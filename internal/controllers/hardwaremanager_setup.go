/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"log/slog"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// setupHardwareManager creates the Kubernetes resources necessary to start the hardware manager.
func (t *reconcilerTask) setupHardwareManager(ctx context.Context, defaultResult ctrl.Result) (nextReconcile ctrl.Result, err error) {

	nextReconcile = defaultResult

	if err = t.createService(ctx, ctlrutils.HardwareManagerServerName, constants.DefaultServicePort, ctlrutils.DefaultServiceTargetPort); err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy Service for the hardware manager server.",
			slog.Any("error", err))
		return
	}

	if err = t.createNetworkPolicy(ctx, ctlrutils.HardwareManagerServerName, constants.DefaultServicePort, ExternalIngress); err != nil {
		t.logger.ErrorContext(ctx, "Failed to create NetworkPolicy for hardware manager server.",
			slog.String("error", err.Error()))
		return
	}

	errorReason, err := t.deployServer(ctx, ctlrutils.HardwareManagerServerName)
	if err != nil {
		t.logger.ErrorContext(ctx, "Failed to deploy the hardware manager server.",
			slog.Any("error", err))
		if errorReason == "" {
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Second}
		}
	}

	return nextReconcile, err
}

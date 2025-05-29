/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resource operations

func UpdateK8sCRStatus(ctx context.Context, c client.Client, object client.Object) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Status().Update(ctx, object); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("status update failed after retries: %w", err)
	}

	return nil
}

//
// Reconciler utilities
//

func DoNotRequeue() ctrl.Result {
	return ctrl.Result{Requeue: false}
}

func RequeueWithLongInterval() ctrl.Result {
	return RequeueWithCustomInterval(5 * time.Minute)
}

func RequeueWithMediumInterval() ctrl.Result {
	return RequeueWithCustomInterval(1 * time.Minute)
}

func RequeueWithShortInterval() ctrl.Result {
	return RequeueWithCustomInterval(15 * time.Second)
}

func RequeueWithCustomInterval(interval time.Duration) ctrl.Result {
	return ctrl.Result{RequeueAfter: interval}
}

func RequeueImmediately() ctrl.Result {
	return ctrl.Result{Requeue: true}
}

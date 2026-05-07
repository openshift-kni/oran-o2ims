/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

func DoNotRequeue() ctrl.Result {
	return ctrl.Result{Requeue: false}
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

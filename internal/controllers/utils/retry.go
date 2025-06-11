/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

func isConflictOrRetriableOrNotFound(err error) bool {
	return isConflictOrRetriable(err) || errors.IsNotFound(err)
}

func isConflictOrRetriable(err error) bool {
	return errors.IsConflict(err) || errors.IsInternalError(err) || errors.IsServiceUnavailable(err) || net.IsConnectionRefused(err)
}

func RetryOnConflictOrRetriable(backoff wait.Backoff, fn func() error) error {
	// nolint: wrapcheck
	return retry.OnError(backoff, isConflictOrRetriable, fn)
}

func RetryOnConflictOrRetriableOrNotFound(backoff wait.Backoff, fn func() error) error {
	// nolint: wrapcheck
	return retry.OnError(backoff, isConflictOrRetriableOrNotFound, fn)
}

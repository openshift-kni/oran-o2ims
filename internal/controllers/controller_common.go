/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import corev1 "k8s.io/api/core/v1"

const (
	controllerTempVolumeName = "tmp"
	controllerTempMountPath  = "/tmp"
	controllerDropAllCaps    = corev1.Capability("ALL")
)

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

// ParentValidationResult holds the result of validating a parent CR reference.
// Used by hierarchy controllers (OCloudSite, ResourcePool) to check if their
// parent resource exists and is ready before marking themselves as ready.
type ParentValidationResult struct {
	Exists bool // Whether the parent resource exists
	Ready  bool // Whether the parent resource has Ready=True condition
}

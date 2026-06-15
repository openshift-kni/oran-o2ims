/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

// ReservedNamespacePrefixes lists namespace name patterns that cannot be
// used as clusterName values. Entries are matched by exact equality or by
// prefix (e.g., "default" matches exactly, "openshift-" matches any name
// starting with "openshift-").
var ReservedNamespacePrefixes = []string{
	"default",
	"kube-",
	"openshift",
	"openshift-",
	"open-cluster-management",
	"multicluster-",
	"ztp-",
}

// IsReservedNamespace checks if a name matches reserved namespace patterns.
// Returns true and the matched pattern if reserved, false otherwise.
func IsReservedNamespace(name string) (bool, string) {
	for _, p := range ReservedNamespacePrefixes {
		if name == p || strings.HasPrefix(name, p) {
			return true, p
		}
	}

	operatorNs := os.Getenv(constants.DefaultNamespaceEnvName)
	if operatorNs == "" {
		operatorNs = constants.DefaultNamespace
	}
	if name == operatorNs {
		return true, operatorNs
	}

	return false, ""
}

// ValidateClusterNameFormat checks that the name is a valid DNS-1123 label.
func ValidateClusterNameFormat(name string) error {
	if name == "" {
		return fmt.Errorf("clusterName is required and must be a non-empty string")
	}

	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		return fmt.Errorf(
			"clusterName %q is not a valid DNS-1123 label: %s",
			name, strings.Join(errs, "; "))
	}

	return nil
}

// ValidateClusterNameNotReserved checks that the name does not target a
// reserved namespace.
func ValidateClusterNameNotReserved(name string) error {
	if reserved, pattern := IsReservedNamespace(name); reserved {
		return fmt.Errorf(
			"clusterName %q targets a reserved namespace (prefix %q)", name, pattern)
	}
	return nil
}

// ValidateClusterNameOwnership checks if a namespace with the given name
// already exists and, if so, verifies it is owned by the specified
// ProvisioningRequest. The ownerLabel parameter is the label key used to
// identify the owning PR. Returns nil if the namespace does not exist or
// is owned by the expected PR.
func ValidateClusterNameOwnership(
	ctx context.Context, c client.Client, clusterName, prName, ownerLabel string) error {

	existing := &corev1.Namespace{}
	if err := c.Get(ctx, client.ObjectKey{Name: clusterName}, existing); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to check for existing namespace %s: %w", clusterName, err)
	}

	if existing.Labels[ownerLabel] != prName {
		return fmt.Errorf(
			"namespace %q already exists and is not owned by ProvisioningRequest %q",
			clusterName, prName)
	}

	return nil
}

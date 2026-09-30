/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"fmt"

	seedvalidation "github.com/openshift-kni/oran-o2ims/internal/validation"
)

const SeedGenerationUnsupportedMessage = "seed generation is not supported by this controller version"

// HasSeedGenerationConfig adapts API resources to the shared validation logic.
func HasSeedGenerationConfig(ct *ClusterTemplate, pr *ProvisioningRequest) (bool, error) {
	configured, err := seedvalidation.HasSeedGenerationConfig(
		ct.Spec.TemplateDefaults.UpgradeDefaults.Raw, pr.Spec.TemplateParameters.Raw)
	if err != nil {
		return false, fmt.Errorf("failed to inspect seed generation input: %w", err)
	}
	return configured, nil
}

// ValidateSeedGenerationUpgradeData delegates merged-input validation to the
// shared package used by both admission and controller code.
func ValidateSeedGenerationUpgradeData(upgradeData map[string]any) error {
	if err := seedvalidation.ValidateSeedGenerationUpgradeData(upgradeData); err != nil {
		return fmt.Errorf("invalid seed generation upgrade data: %w", err)
	}
	return nil
}

// ValidateSeedGenerationPRSchema delegates template schema rules to the
// shared validation package.
func ValidateSeedGenerationPRSchema(schema map[string]any) error {
	if err := seedvalidation.ValidateSeedGenerationPRSchema(schema); err != nil {
		return fmt.Errorf("invalid seed generation PR schema: %w", err)
	}
	return nil
}

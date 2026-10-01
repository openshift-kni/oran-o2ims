/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

// Package upgrade contains shared upgrade-parameter handling used by admission
// and controller reconciliation.
package upgrade

import (
	"encoding/json"
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

// RequestedWorkerPoolUpgradeThrough resolves the authorized Custom stage from
// ClusterTemplate defaults and ProvisioningRequest parameters. An explicitly
// present PR value, including an empty string, overrides the template default.
func RequestedWorkerPoolUpgradeThrough(defaultsRaw, paramsRaw []byte) (string, error) {
	readUpgradeThrough := func(raw []byte, source string, nested bool) (string, bool, error) {
		if len(raw) == 0 {
			return "", false, nil
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			return "", false, fmt.Errorf("failed to parse %s: %w", source, err)
		}
		if data == nil {
			return "", false, fmt.Errorf("%s must be an object", source)
		}
		location := source
		if nested {
			value, found := data[constants.TemplateParamUpgrade]
			if !found {
				return "", false, nil
			}
			location += "." + constants.TemplateParamUpgrade
			var ok bool
			data, ok = value.(map[string]any)
			if !ok {
				return "", false, fmt.Errorf("%s must be an object", location)
			}
		}
		cvValue, found := data["clusterVersion"]
		if !found {
			return "", false, nil
		}
		location += ".clusterVersion"
		var ok bool
		data, ok = cvValue.(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("%s must be an object", location)
		}
		poolValue, found := data["workerPoolUpgrade"]
		if !found {
			return "", false, nil
		}
		pool, ok := poolValue.(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("%s.workerPoolUpgrade must be an object", location)
		}
		value, found := pool["upgradeThrough"]
		if !found {
			return "", false, nil
		}
		through, ok := value.(string)
		if !ok {
			return "", false, fmt.Errorf("%s.workerPoolUpgrade.upgradeThrough must be a string", location)
		}
		return through, true, nil
	}

	through, _, err := readUpgradeThrough(defaultsRaw, "ClusterTemplate upgradeDefaults", false)
	if err != nil {
		return "", err
	}
	override, found, err := readUpgradeThrough(paramsRaw, "templateParameters", true)
	if err != nil {
		return "", err
	}
	if found {
		through = override
	}
	return through, nil
}

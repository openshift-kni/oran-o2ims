/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestProvisioningApiSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provisioning API Suite")
}

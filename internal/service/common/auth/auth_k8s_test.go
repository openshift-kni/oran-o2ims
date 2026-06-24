/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("KubernetesAuthenticatorConfig", func() {
	It("should propagate audiences to the config", func() {
		config := KubernetesAuthenticatorConfig{
			Audiences: []string{"cluster-server", "resource-server"},
		}
		Expect(config.Audiences).To(HaveLen(2))
		Expect(config.Audiences).To(ContainElement("cluster-server"))
		Expect(config.Audiences).To(ContainElement("resource-server"))
	})

	It("should work with empty audiences", func() {
		config := KubernetesAuthenticatorConfig{
			Audiences: []string{},
		}
		Expect(config.Audiences).To(BeEmpty())
	})
})

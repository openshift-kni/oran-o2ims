/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHierarchySuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hierarchy Helpers Suite")
}

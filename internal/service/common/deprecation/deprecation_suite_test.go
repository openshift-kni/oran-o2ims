/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package deprecation_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDeprecation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Deprecation Suite")
}

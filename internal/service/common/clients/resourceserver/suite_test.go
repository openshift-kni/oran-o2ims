/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package resourceserver

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestResourceServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resource Server Client Suite")
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package alertmanager

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDictionary(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alertmanager Suite")
}

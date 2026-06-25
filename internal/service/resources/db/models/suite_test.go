/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestModelsSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resource Models Suite")
}

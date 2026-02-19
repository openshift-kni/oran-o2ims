/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resources API Suite")
}

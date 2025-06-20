/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package async

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAsync(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Async Suite")
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package notifier_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNotifier(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Common/Notifier")
}

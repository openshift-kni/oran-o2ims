/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package clusterserver

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClusterServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Server Client Suite")
}

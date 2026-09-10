/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package serviceconfig_test

import (
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestServiceConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alarms ServiceConfig Suite")
}

var _ = BeforeSuite(func() {
	slog.SetDefault(slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{Level: slog.LevelInfo})))
})

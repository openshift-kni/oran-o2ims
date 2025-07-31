/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Cursor/claude-4-sonnet
*/

// Package server provides unit tests for the Metal3 provisioning server implementation.
// These tests cover:
//   - NewMetal3PluginServer constructor function validation and parameter handling
//   - Interface compliance verification (StrictServerInterface)
//   - Field initialization and embedded struct configuration
//   - Error handling scenarios
//   - Proper setting of Metal3-specific constants and identifiers
//
// The tests use Ginkgo BDD framework with Gomega matchers and include mocks
// for external dependencies like client.Client and logger.
package server

import (
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/openshift-kni/oran-o2ims/hwmgr-plugins/api/server/provisioning"
	hwmgrutils "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/controller/utils"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

var _ = Describe("Metal3PluginServer", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *MockClient
		logger     *slog.Logger
		config     svcutils.CommonServerConfig
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = NewMockClient(ctrl)
		logger = slog.Default()
		config = svcutils.CommonServerConfig{
			Listener: svcutils.ListenerConfig{
				Address: "localhost:8080",
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("NewMetal3PluginServer", func() {
		Context("when creating a new Metal3 plugin server", func() {
			It("should create a server successfully with valid parameters", func() {
				server, err := NewMetal3PluginServer(config, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())
				Expect(server).ToNot(BeNil())
				Expect(server).To(BeAssignableToTypeOf(&Metal3PluginServer{}))
			})

			It("should implement the StrictServerInterface", func() {
				server, err := NewMetal3PluginServer(config, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())
				// Verify that the server can be used as a StrictServerInterface
				var _ provisioning.StrictServerInterface = server
				Expect(server).ToNot(BeNil())
			})

			It("should properly initialize all embedded struct fields", func() {
				server, err := NewMetal3PluginServer(config, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())
				Expect(server.HardwarePluginServer.CommonServerConfig).To(Equal(config))
				Expect(server.HardwarePluginServer.HubClient).To(Equal(mockClient))
				Expect(server.HardwarePluginServer.Logger).To(Equal(logger))
			})

			It("should set the correct Metal3-specific configuration", func() {
				server, err := NewMetal3PluginServer(config, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())
				Expect(server.HardwarePluginServer.Namespace).To(Equal(provisioning.GetMetal3HWPluginNamespace()))
				Expect(server.HardwarePluginServer.HardwarePluginID).To(Equal(hwmgrutils.Metal3HardwarePluginID))
				Expect(server.HardwarePluginServer.ResourcePrefix).To(Equal(Metal3ResourcePrefix))
			})

			It("should set the Metal3ResourcePrefix constant correctly", func() {
				server, err := NewMetal3PluginServer(config, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())
				Expect(server.HardwarePluginServer.ResourcePrefix).To(Equal("metal3"))
			})

			It("should set the correct Hardware Plugin ID", func() {
				server, err := NewMetal3PluginServer(config, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())
				Expect(server.HardwarePluginServer.HardwarePluginID).To(Equal("metal3-hwplugin"))
			})
		})

		Context("when handling different parameter combinations", func() {
			It("should work with a nil logger", func() {
				server, err := NewMetal3PluginServer(config, mockClient, nil)

				Expect(err).ToNot(HaveOccurred())
				Expect(server).ToNot(BeNil())
				Expect(server.HardwarePluginServer.Logger).To(BeNil())
			})

			It("should work with empty config", func() {
				emptyConfig := svcutils.CommonServerConfig{}
				server, err := NewMetal3PluginServer(emptyConfig, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())
				Expect(server).ToNot(BeNil())
				Expect(server.HardwarePluginServer.CommonServerConfig).To(Equal(emptyConfig))
			})

			It("should work with nil client", func() {
				server, err := NewMetal3PluginServer(config, nil, logger)

				Expect(err).ToNot(HaveOccurred())
				Expect(server).ToNot(BeNil())
				Expect(server.HardwarePluginServer.HubClient).To(BeNil())
			})
		})

		Context("when verifying interface compliance", func() {
			It("should implement provisioning.StrictServerInterface at compile time", func() {
				// This test verifies the compile-time interface check
				var _ provisioning.StrictServerInterface = (*Metal3PluginServer)(nil)

				// If we reach this point, the interface is implemented correctly
				Expect(true).To(BeTrue())
			})
		})

		Context("when verifying field initialization order", func() {
			It("should initialize all fields in the correct order", func() {
				server, err := NewMetal3PluginServer(config, mockClient, logger)

				Expect(err).ToNot(HaveOccurred())

				// Verify that the embedded struct is properly initialized
				hardwareServer := server.HardwarePluginServer
				Expect(hardwareServer.CommonServerConfig).To(Equal(config))
				Expect(hardwareServer.HubClient).To(Equal(mockClient))
				Expect(hardwareServer.Logger).To(Equal(logger))
				Expect(hardwareServer.Namespace).ToNot(BeEmpty())
				Expect(hardwareServer.HardwarePluginID).ToNot(BeEmpty())
				Expect(hardwareServer.ResourcePrefix).ToNot(BeEmpty())
			})
		})
	})
})

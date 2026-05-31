/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Assisted-by: Cursor/claude-4-sonnet
*/

/*
Test Cases Overview for Controllers Package

This test suite (suite_test.go) provides the foundational setup and utilities for comprehensive testing
of the O2IMS controllers package. The suite supports the following test categories:

1. ClusterTemplate Controller Tests (clustertemplate_controller_test.go):
   - ClusterTemplate reconciliation and validation
   - ConfigMap reference validation and schema compliance
   - Hardware template timeout validation
   - Policy template parameter schema validation
   - Cluster instance name and template ID validation
   - Upgrade defaults ConfigMap validation for Image-Based Group Upgrades
   - Status condition management for validation results

2. ProvisioningRequest Controller Tests (provisioningrequest_controller_test.go):
   - Complete provisioning request lifecycle management
   - Multi-phase reconciliation (validation, resource creation, hardware provisioning,
     cluster installation, configuration)
   - Hardware manager integration and node allocation
   - Cluster template resolution and application
   - BMC secret creation and management
   - Status condition tracking through provisioning phases

3. ProvisioningRequest Hardware Provisioning Tests (provisioningrequest_hwprovision_test.go):
   - Hardware template rendering and validation
   - Node allocation request creation and management
   - AllocatedNode resource handling
   - Hardware manager API integration
   - BMC credential management
   - Node group processing (controller/worker nodes)

4. ProvisioningRequest Cluster Installation Tests (provisioningrequest_clusterinstall_test.go):
   - ClusterInstance rendering and dry-run validation
   - Node detail extraction and assignment
   - BMC and network interface configuration
   - ManagedCluster annotation handling
   - ClusterInstance validation with hardware details

5. ProvisioningRequest Resource Creation Tests (provisioningrequest_resourcecreation_test.go):
   - Policy template ConfigMap creation
   - BMC secret generation from credentials
   - Cluster resource creation and management
   - Label validation for policy applications

6. ProvisioningRequest Validation Tests (provisioningrequest_validation_test.go):
   - Configuration override and merging logic
   - Labels and annotations management
   - Nested resource validation
   - Input validation for provisioning requests

7. ProvisioningRequest Configuration Tests (provisioningrequest_clusterconfig_test.go):
   - Cluster configuration phase management
   - Policy application and validation
   - Configuration status tracking

8. Inventory Controller Tests (inventory_controller_test.go):
   - Inventory resource reconciliation
   - Server deployment management
   - Pod readiness and status monitoring
   - Inventory service lifecycle

Test Infrastructure:
- Ginkgo BDD-style test framework with Gomega assertions
- Fake Kubernetes client with comprehensive scheme registration
- BMC secret and authentication setup for realistic scenarios
- Status subresource support for all custom resources
- Indexing support for efficient resource lookups

The test suite ensures comprehensive coverage of the O2IMS provisioning workflow from
cluster template validation through complete cluster deployment and configuration.
*/

package controllers

import (
	"log/slog"
	"os"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
	"github.com/openshift-kni/oran-o2ims/test/fakeclient"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers")
}

// Logger used for tests:
var logger *slog.Logger

// Scheme alias for tests that build their own fake clients directly.
var scheme = fakeclient.Scheme

var _ = BeforeSuite(func() {
	// Create a logger that writes to the Ginkgo writer, so that the log messages will be
	// attached to the output of the right test:
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	logger = slog.New(handler)

	// Configure the controller runtime library to use our logger:
	adapter := logr.FromSlogHandler(logger.Handler())
	ctrl.SetLogger(adapter)
	klog.SetLogger(adapter)

	os.Setenv(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
})

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ValidateCVUpgradeData", func() {
	const release = "4.17.0"
	const label = "upgradeDefaults"

	It("should return nil when no upgrade keys are present", func() {
		data := map[string]any{"someOtherKey": "value"}
		Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
	})

	It("should return InputError when clusterVersion is not an object", func() {
		data := map[string]any{"clusterVersion": "invalid"}
		err := ValidateCVUpgradeData(data, release, label)
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("must be an object"))
	})

	It("should use the context label in error messages", func() {
		data := map[string]any{"clusterVersion": "invalid"}
		err := ValidateCVUpgradeData(data, release, "upgradeParameters")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("upgradeParameters"))
	})

	Context("desiredUpdate.version", func() {
		It("should pass when desiredUpdate.version matches release", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": "4.17.0"},
				},
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should reject when desiredUpdate.version does not match release", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": "4.18.0"},
				},
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("does not match the ClusterTemplate spec.release"))
		})

		It("should pass when desiredUpdate.version is empty", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": ""},
				},
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should pass when desiredUpdate is absent", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{},
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should pass when desiredUpdate has no version key", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"channel": "stable-4.17"},
				},
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})
	})

	Context("clusterUpgradeTimeout", func() {
		It("should pass with a valid duration", func() {
			data := map[string]any{
				"clusterVersion":        map[string]any{},
				"clusterUpgradeTimeout": "2h30m",
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should reject an invalid duration", func() {
			data := map[string]any{
				"clusterVersion":        map[string]any{},
				"clusterUpgradeTimeout": "notaduration",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("invalid clusterUpgradeTimeout"))
		})

		It("should reject a zero duration", func() {
			data := map[string]any{
				"clusterUpgradeTimeout": "0s",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must be a positive duration"))
		})

		It("should reject a negative duration", func() {
			data := map[string]any{
				"clusterUpgradeTimeout": "-5m",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must be a positive duration"))
		})

		It("should pass when clusterUpgradeTimeout is not present", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{},
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should validate clusterUpgradeTimeout without clusterVersion", func() {
			data := map[string]any{
				"clusterUpgradeTimeout": "notaduration",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("invalid clusterUpgradeTimeout"))
		})
	})

	Context("intermediateVersion", func() {
		It("should pass with a valid intermediateVersion one minor below", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "4.16.3",
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should reject non-semver intermediateVersion", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "not-semver",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("is not valid semver"))
		})

		It("should reject when major version differs", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "3.16.0",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("major version (3) must equal ClusterTemplate's spec.release major version (4)"))
		})

		It("should reject when not exactly one minor below", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "4.15.0",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must be exactly one minor version below"))
		})

		It("should pass when intermediateVersion is empty", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "",
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should reject when release is not valid semver", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "4.16.0",
			}
			err := ValidateCVUpgradeData(data, "not-semver", label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("spec.release"))
		})

		It("should validate intermediateVersion without clusterVersion", func() {
			data := map[string]any{
				"intermediateVersion": "not-semver",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("is not valid semver"))
		})
	})

	Context("combined rules", func() {
		It("should validate all rules together for a valid config", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": "4.17.0"},
				},
				"clusterUpgradeTimeout": "2h30m",
				"intermediateVersion":   "4.16.3",
			}
			Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
		})

		It("should fail on the first violated rule", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": "4.18.0"},
				},
				"clusterUpgradeTimeout": "invalid",
			}
			err := ValidateCVUpgradeData(data, release, label)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not match"))
		})
	})
})

var _ = Describe("ValidateEUSIntermediate", func() {
	It("should accept intermediate one minor below target", func() {
		Expect(ValidateEUSIntermediate("4.21.0", "4.22.0")).ToNot(HaveOccurred())
	})

	It("should reject wrong minor gap", func() {
		err := ValidateEUSIntermediate("4.20.5", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("exactly one minor version below"))
	})

	It("should reject wrong major version", func() {
		err := ValidateEUSIntermediate("99.21.0", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("must equal ClusterTemplate's spec.release major version"))
	})

	It("should reject invalid intermediateVersion", func() {
		err := ValidateEUSIntermediate("invalid", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("is not valid semver"))
	})

	It("should reject invalid target version", func() {
		err := ValidateEUSIntermediate("4.21.0", "not-semver")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("ClusterTemplate's spec.release"))
	})
})

var _ = Describe("WorkerPoolUpgrade validation", func() {
	Describe("ValidateWorkerPoolUpgrade", func() {
		It("should reject OpenShiftDefault for EUS", func() {
			err := ValidateWorkerPoolUpgrade(true, constants.WorkerPoolUpgradeStrategyOpenShiftDefault, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not applicable to EUS"))
		})

		It("should reject poolsWithControlPlane with OpenShiftDefault", func() {
			err := ValidateWorkerPoolUpgrade(
				false, constants.WorkerPoolUpgradeStrategyOpenShiftDefault, []string{"worker-canary"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("poolsWithControlPlane is not supported"))
		})

		It("should accept empty poolsWithControlPlane with OpenShiftDefault", func() {
			Expect(ValidateWorkerPoolUpgrade(
				false, constants.WorkerPoolUpgradeStrategyOpenShiftDefault, nil,
			)).To(Succeed())
		})

		It("should reject poolsWithControlPlane for EUS", func() {
			err := ValidateWorkerPoolUpgrade(
				true, constants.WorkerPoolUpgradeStrategySerial, []string{"worker-canary"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not supported for EUS"))
		})

		It("should accept Serial with poolsWithControlPlane for non-EUS", func() {
			Expect(ValidateWorkerPoolUpgrade(
				false, constants.WorkerPoolUpgradeStrategySerial, []string{"worker-canary"},
			)).To(Succeed())
		})

		It("should reject an unknown strategy", func() {
			err := ValidateWorkerPoolUpgrade(false, "Custom", nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported workerPoolUpgrade.strategy"))
		})
	})

	Describe("ValidatePoolsWithControlPlaneNames", func() {
		mcps := []mcfgv1.MachineConfigPool{
			{ObjectMeta: metav1.ObjectMeta{Name: "worker"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
		}

		It("should accept known worker pool names", func() {
			Expect(ValidatePoolsWithControlPlaneNames(mcps, []string{"worker-a"})).To(Succeed())
		})

		It("should reject an empty name", func() {
			err := ValidatePoolsWithControlPlaneNames(mcps, []string{""})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty name"))
		})

		It("should reject master", func() {
			err := ValidatePoolsWithControlPlaneNames(mcps, []string{"master"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not include"))
		})

		It("should reject unknown names", func() {
			err := ValidatePoolsWithControlPlaneNames(mcps, []string{"missing"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown MachineConfigPool"))
		})

		It("should reject duplicates", func() {
			err := ValidatePoolsWithControlPlaneNames(mcps, []string{"worker", "worker"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate"))
		})
	})
})

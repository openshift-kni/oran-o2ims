/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
)

var _ = Describe("ValidateCVUpgradeData", func() {
	const release = "4.17.0"

	It("should return nil when clusterVersion key is absent", func() {
		data := map[string]any{"clusterUpgradeTimeout": "2h"}
		Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
	})

	It("should return InputError when clusterVersion is not an object", func() {
		data := map[string]any{"clusterVersion": "invalid"}
		err := ValidateCVUpgradeData(data, release)
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("must be an object"))
	})

	Context("desiredUpdate.version", func() {
		It("should pass when desiredUpdate.version matches release", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": "4.17.0"},
				},
			}
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})

		It("should reject when desiredUpdate.version does not match release", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": "4.18.0"},
				},
			}
			err := ValidateCVUpgradeData(data, release)
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
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})

		It("should pass when desiredUpdate is absent", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{},
			}
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})

		It("should pass when desiredUpdate has no version key", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"channel": "stable-4.17"},
				},
			}
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})
	})

	Context("clusterUpgradeTimeout", func() {
		It("should pass with a valid duration", func() {
			data := map[string]any{
				"clusterVersion":        map[string]any{},
				"clusterUpgradeTimeout": "2h30m",
			}
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})

		It("should reject an invalid duration", func() {
			data := map[string]any{
				"clusterVersion":        map[string]any{},
				"clusterUpgradeTimeout": "notaduration",
			}
			err := ValidateCVUpgradeData(data, release)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("invalid clusterUpgradeTimeout"))
		})

		It("should pass when clusterUpgradeTimeout is not present", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{},
			}
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})
	})

	Context("intermediateVersion", func() {
		It("should pass with a valid intermediateVersion one minor below", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "4.16.3",
			}
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})

		It("should reject non-semver intermediateVersion", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "not-semver",
			}
			err := ValidateCVUpgradeData(data, release)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("invalid intermediateVersion"))
		})

		It("should reject when major version differs", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "3.16.0",
			}
			err := ValidateCVUpgradeData(data, release)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("major version (3) must equal spec.release major version (4)"))
		})

		It("should reject when not exactly one minor below", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "4.15.0",
			}
			err := ValidateCVUpgradeData(data, release)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("must be exactly one minor version below"))
		})

		It("should pass when intermediateVersion is empty", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "",
			}
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})

		It("should reject when release is not valid semver", func() {
			data := map[string]any{
				"clusterVersion":      map[string]any{},
				"intermediateVersion": "4.16.0",
			}
			err := ValidateCVUpgradeData(data, "not-semver")
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("spec.release"))
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
			Expect(ValidateCVUpgradeData(data, release)).ToNot(HaveOccurred())
		})

		It("should fail on the first violated rule", func() {
			data := map[string]any{
				"clusterVersion": map[string]any{
					"desiredUpdate": map[string]any{"version": "4.18.0"},
				},
				"clusterUpgradeTimeout": "invalid",
			}
			err := ValidateCVUpgradeData(data, release)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not match"))
		})
	})
})

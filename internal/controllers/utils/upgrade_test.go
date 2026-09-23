/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Upgrade helper functions", func() {
	Describe("FindCVHistoryEntry", func() {
		cv := &configv1.ClusterVersion{
			Status: configv1.ClusterVersionStatus{
				History: []configv1.UpdateHistory{
					{Version: "4.22.0", State: configv1.CompletedUpdate},
					{Version: "4.21.3", State: configv1.CompletedUpdate},
				},
			},
		}

		It("should return entry when version is found", func() {
			entry := FindCVHistoryEntry(cv, "4.22.0")
			Expect(entry).ToNot(BeNil())
			Expect(entry.Version).To(Equal("4.22.0"))
			Expect(entry.State).To(Equal(configv1.CompletedUpdate))
		})

		It("should return nil when version is not found", func() {
			entry := FindCVHistoryEntry(cv, "4.23.0")
			Expect(entry).To(BeNil())
		})
	})

	Describe("IsCVUpdateAvailable", func() {
		cv := &configv1.ClusterVersion{
			Status: configv1.ClusterVersionStatus{
				AvailableUpdates: []configv1.Release{
					{Version: "4.21.3", Image: "quay.io/openshift/4.21.3"},
					{Version: "4.22.0", Image: "quay.io/openshift/4.22.0"},
				},
			},
		}

		It("should return true when version is found", func() {
			Expect(IsCVUpdateAvailable(cv, "4.22.0")).To(BeTrue())
		})

		It("should return false when version is not found", func() {
			Expect(IsCVUpdateAvailable(cv, "4.23.0")).To(BeFalse())
		})
	})

	Describe("IsMajorOrMinorUpgrade", func() {
		It("should return true for minor version upgrade", func() {
			isMinorOrMajor, err := IsMajorOrMinorUpgrade("4.21.3", "4.22.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isMinorOrMajor).To(BeTrue())
		})

		It("should return true for major version upgrade", func() {
			isMinorOrMajor, err := IsMajorOrMinorUpgrade("4.22.0", "5.0.1")
			Expect(err).ToNot(HaveOccurred())
			Expect(isMinorOrMajor).To(BeTrue())
		})

		It("should return false for z-stream upgrade", func() {
			isMinorOrMajor, err := IsMajorOrMinorUpgrade("4.21.0", "4.21.3")
			Expect(err).ToNot(HaveOccurred())
			Expect(isMinorOrMajor).To(BeFalse())
		})

		It("should return false for same version", func() {
			isMinorOrMajor, err := IsMajorOrMinorUpgrade("4.21.0", "4.21.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isMinorOrMajor).To(BeFalse())
		})

		It("should return false for major version downgrade", func() {
			isMinorOrMajor, err := IsMajorOrMinorUpgrade("5.0.1", "4.22.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isMinorOrMajor).To(BeFalse())
		})

		It("should return false with no error for empty current version", func() {
			isMinorOrMajor, err := IsMajorOrMinorUpgrade("", "4.22.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isMinorOrMajor).To(BeFalse())
		})

		It("should return false with no error for empty target version", func() {
			isMinorOrMajor, err := IsMajorOrMinorUpgrade("4.21.0", "")
			Expect(err).ToNot(HaveOccurred())
			Expect(isMinorOrMajor).To(BeFalse())
		})

		It("should return error for invalid current version", func() {
			_, err := IsMajorOrMinorUpgrade("invalid", "4.22.0")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse current version"))
		})

		It("should return error for invalid target version", func() {
			_, err := IsMajorOrMinorUpgrade("4.21.0", "invalid")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse target version"))
		})
	})

	Describe("GetCVCondition", func() {
		cv := &configv1.ClusterVersion{
			Status: configv1.ClusterVersionStatus{
				Conditions: []configv1.ClusterOperatorStatusCondition{
					{
						Type:    configv1.OperatorProgressing,
						Status:  configv1.ConditionTrue,
						Message: "Working towards 4.22.0",
					},
					{
						Type:    configv1.OperatorUpgradeable,
						Status:  configv1.ConditionFalse,
						Message: "Cluster operator X is not upgradeable",
					},
				},
			},
		}

		It("should return condition when found", func() {
			cond := GetCVCondition(cv, configv1.OperatorProgressing)
			Expect(cond).ToNot(BeNil())
			Expect(cond.Status).To(Equal(configv1.ConditionTrue))
		})

		It("should return nil when not found", func() {
			cond := GetCVCondition(cv, configv1.RetrievedUpdates)
			Expect(cond).To(BeNil())
		})

		It("should return nil when cv is nil", func() {
			cond := GetCVCondition(nil, configv1.OperatorProgressing)
			Expect(cond).To(BeNil())
		})
	})

	Describe("IsEUSUpgrade", func() {
		It("should return true for 4.20->4.22", func() {
			isEUS, err := IsEUSUpgrade("4.20.5", "4.22.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeTrue())
		})

		It("should return true for 4.18->4.20", func() {
			isEUS, err := IsEUSUpgrade("4.18.0", "4.20.3")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeTrue())
		})

		It("should return false for 4.20->4.21 (odd target)", func() {
			isEUS, err := IsEUSUpgrade("4.20.0", "4.21.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeFalse())
		})

		It("should return false for 4.21->4.23 (odd current)", func() {
			isEUS, err := IsEUSUpgrade("4.21.0", "4.23.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeFalse())
		})

		It("should return false for 4.20->4.24 (gap of 4)", func() {
			isEUS, err := IsEUSUpgrade("4.20.0", "4.24.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeFalse())
		})

		It("should return false for cross-major 3.20->4.22", func() {
			isEUS, err := IsEUSUpgrade("3.20.0", "4.22.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeFalse())
		})

		It("should return false for z-stream 4.20.0->4.20.3", func() {
			isEUS, err := IsEUSUpgrade("4.20.0", "4.20.3")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeFalse())
		})

		It("should return false with no error for empty start version", func() {
			isEUS, err := IsEUSUpgrade("", "4.22.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(isEUS).To(BeFalse())
		})

		It("should return error for invalid start version", func() {
			_, err := IsEUSUpgrade("invalid", "4.22.0")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse start version"))
		})
	})

	Describe("ResolveCVUpgradeAction", func() {
		Context("standard upgrade (z-stream)", func() {
			It("should return target/PreStart when no history", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.22.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.3", "", false)
				Expect(action.UpgradeToVersion).To(Equal("4.22.3"))
				Expect(action.Phase).To(Equal(PhasePreStart))
				Expect(action.IsEUS).To(BeFalse())
			})

			It("should return target/Completed when target completed", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.22.3", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.3", "", false)
				Expect(action.Phase).To(Equal(PhaseCompleted))
			})

			It("should return target/InProgress when target partial", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.22.3", State: configv1.PartialUpdate},
							{Version: "4.22.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.3", "", false)
				Expect(action.Phase).To(Equal(PhaseInProgress))
			})
		})

		Context("EUS upgrade", func() {
			It("should return empty intermediate/PreStart when status intermediate is empty", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.20.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.0", "", true)
				Expect(action.UpgradeToVersion).To(Equal(""))
				Expect(action.Phase).To(Equal(PhasePreStart))
				Expect(action.IsEUS).To(BeTrue())
				Expect(action.IsEUSIntermediate("")).To(BeTrue())
			})

			It("should return intermediate/PreStart when no intermediate history", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.20.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.0", "4.21.0", true)
				Expect(action.UpgradeToVersion).To(Equal("4.21.0"))
				Expect(action.Phase).To(Equal(PhasePreStart))
				Expect(action.IsEUS).To(BeTrue())
				Expect(action.IsEUSIntermediate("4.21.0")).To(BeTrue())
			})

			It("should return intermediate/InProgress when intermediate partial", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.21.0", State: configv1.PartialUpdate},
							{Version: "4.20.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.0", "4.21.0", true)
				Expect(action.UpgradeToVersion).To(Equal("4.21.0"))
				Expect(action.Phase).To(Equal(PhaseInProgress))
				Expect(action.IsEUSIntermediate("4.21.0")).To(BeTrue())
			})

			It("should return target/PreStart when intermediate completed", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.21.0", State: configv1.CompletedUpdate},
							{Version: "4.20.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.0", "4.21.0", true)
				Expect(action.UpgradeToVersion).To(Equal("4.22.0"))
				Expect(action.Phase).To(Equal(PhasePreStart))
				Expect(action.IsEUSIntermediate("4.21.0")).To(BeFalse())
			})

			It("should return target/InProgress when target partial", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.22.0", State: configv1.PartialUpdate},
							{Version: "4.21.0", State: configv1.CompletedUpdate},
							{Version: "4.20.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.0", "4.21.0", true)
				Expect(action.UpgradeToVersion).To(Equal("4.22.0"))
				Expect(action.Phase).To(Equal(PhaseInProgress))
				Expect(action.IsEUSIntermediate("4.21.0")).To(BeFalse())
			})

			It("should return target/Completed when both completed", func() {
				cv := &configv1.ClusterVersion{
					Status: configv1.ClusterVersionStatus{
						History: []configv1.UpdateHistory{
							{Version: "4.22.0", State: configv1.CompletedUpdate},
							{Version: "4.21.0", State: configv1.CompletedUpdate},
							{Version: "4.20.0", State: configv1.CompletedUpdate},
						},
					},
				}
				action := ResolveCVUpgradeAction(cv, "4.22.0", "4.21.0", true)
				Expect(action.Phase).To(Equal(PhaseCompleted))
			})
		})
	})
})

var _ = Describe("MachineConfigPool helpers", func() {
	mcpNames := func(mcps []mcfgv1.MachineConfigPool) []string {
		names := make([]string, 0, len(mcps))
		for i := range mcps {
			names = append(names, mcps[i].Name)
		}
		return names
	}

	DescribeTable("GetNonUpdatedMCPs",
		func(generation, observedGeneration int64, status corev1.ConditionStatus, expected []string) {
			mcp := mcfgv1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{Name: "worker", Generation: generation},
				Status: mcfgv1.MachineConfigPoolStatus{
					ObservedGeneration: observedGeneration,
					Conditions: []mcfgv1.MachineConfigPoolCondition{
						{Type: mcfgv1.MachineConfigPoolUpdated, Status: status},
					},
				},
			}
			logger := slog.New(slog.DiscardHandler)
			Expect(GetNonUpdatedMCPs(context.Background(), logger, []mcfgv1.MachineConfigPool{mcp})).To(
				Equal(expected))
		},
		Entry("when Updated=True is for the current generation",
			int64(2), int64(2), corev1.ConditionTrue, []string(nil)),
		Entry("when Updated=True is stale",
			int64(2), int64(1), corev1.ConditionTrue, []string{"worker"}),
		Entry("when the current generation has Updated=False",
			int64(2), int64(2), corev1.ConditionFalse, []string{"worker"}),
	)

	Describe("ExcludeMCPsByNames", func() {
		It("should return remaining pools in alphabetical order", func() {
			mcps := []mcfgv1.MachineConfigPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-b"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-canary"}},
			}
			got := ExcludeMCPsByNames(mcps, []string{"worker-canary"})
			Expect(mcpNames(got)).To(Equal([]string{"worker-a", "worker-b"}))
		})

		It("should sort all pools when the exclusion list is empty", func() {
			mcps := []mcfgv1.MachineConfigPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-b"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
			}
			Expect(mcpNames(ExcludeMCPsByNames(mcps, nil))).To(
				Equal([]string{"worker-a", "worker-b"}))
		})

		It("should ignore unknown names", func() {
			mcps := []mcfgv1.MachineConfigPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-b"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
			}
			Expect(mcpNames(ExcludeMCPsByNames(mcps, []string{"missing"}))).To(
				Equal([]string{"worker-a", "worker-b"}))
		})
	})

	Describe("FilterMCPsByNames", func() {
		It("should return matching pools in the requested order", func() {
			mcps := []mcfgv1.MachineConfigPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-b"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-canary"}},
			}
			got := FilterMCPsByNames(mcps, []string{"worker-canary", "worker-a"})
			Expect(mcpNames(got)).To(Equal([]string{"worker-canary", "worker-a"}))
		})

		It("should ignore unknown names", func() {
			mcps := []mcfgv1.MachineConfigPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-b"}},
			}
			got := FilterMCPsByNames(mcps, []string{"missing", "worker-b"})
			Expect(mcpNames(got)).To(Equal([]string{"worker-b"}))
		})

		It("should return no pools when the requested names are empty", func() {
			mcps := []mcfgv1.MachineConfigPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
			}
			Expect(FilterMCPsByNames(mcps, nil)).To(BeEmpty())
		})
	})
})

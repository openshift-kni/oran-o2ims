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
		withControlPlane := func(strategy string) WorkerPoolUpgrade {
			config := WorkerPoolUpgrade{
				Strategy:              strategy,
				PoolsWithControlPlane: []string{"worker-canary"},
			}
			if strategy == constants.WorkerPoolUpgradeStrategyCustom {
				config.Stages = []WorkerPoolUpgradeStage{{Name: "rest", Pools: []string{"worker-rest"}}}
			}
			return config
		}

		It("should reject OpenShiftDefault for EUS", func() {
			err := ValidateWorkerPoolUpgrade(true, WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyOpenShiftDefault,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not applicable to EUS"))
		})

		It("should reject poolsWithControlPlane with OpenShiftDefault", func() {
			err := ValidateWorkerPoolUpgrade(false, WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyOpenShiftDefault,
				PoolsWithControlPlane: []string{"worker-canary"},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("poolsWithControlPlane is not supported"))
		})

		It("should accept empty poolsWithControlPlane with OpenShiftDefault", func() {
			Expect(ValidateWorkerPoolUpgrade(false, WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyOpenShiftDefault,
			})).To(Succeed())
		})

		DescribeTable("rejects poolsWithControlPlane for EUS upgrades",
			func(strategy string) {
				Expect(ValidateWorkerPoolUpgrade(true, withControlPlane(strategy))).To(
					MatchError(ContainSubstring("poolsWithControlPlane is not supported for EUS upgrades")))
			},
			Entry("Serial", constants.WorkerPoolUpgradeStrategySerial),
			Entry("Parallel", constants.WorkerPoolUpgradeStrategyParallel),
			Entry("Custom", constants.WorkerPoolUpgradeStrategyCustom),
		)

		DescribeTable("accepts poolsWithControlPlane for non-EUS upgrades",
			func(strategy string) {
				Expect(ValidateWorkerPoolUpgrade(false, withControlPlane(strategy))).To(Succeed())
			},
			Entry("Serial", constants.WorkerPoolUpgradeStrategySerial),
			Entry("Parallel", constants.WorkerPoolUpgradeStrategyParallel),
			Entry("Custom", constants.WorkerPoolUpgradeStrategyCustom),
		)

		It("should reject an unknown strategy", func() {
			err := ValidateWorkerPoolUpgrade(false, WorkerPoolUpgrade{Strategy: "Unknown"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported workerPoolUpgrade.strategy"))
		})

		It("should accept a complete Custom configuration", func() {
			config := WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
					{Name: "rest", Pools: []string{"worker-b", "worker-c"}},
				},
				UpgradeThrough: "canary",
			}
			Expect(ValidateWorkerPoolUpgrade(false, config)).To(Succeed())
		})

		DescribeTable("rejects a Custom plan with an invalid stage or pool assignment",
			func(config WorkerPoolUpgrade, expected string) {
				err := ValidateWorkerPoolUpgrade(false, config)
				Expect(err).To(MatchError(expected))
			},
			Entry("no stages", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
			}, "workerPoolUpgrade.stages must not be empty with strategy Custom"),
			Entry("empty stage name", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Pools: []string{"worker-a"}}},
			}, "workerPoolUpgrade.stages contains an empty stage name"),
			Entry("duplicate stage name", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "same", Pools: []string{"worker-a"}},
					{Name: "same", Pools: []string{"worker-b"}},
				},
			}, "workerPoolUpgrade.stages contains duplicate stage name \"same\""),
			Entry("stage with no pools", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Name: "canary"}},
			}, "workerPoolUpgrade stage \"canary\" must contain at least one pool"),
			Entry("empty pool name in a stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Name: "canary", Pools: []string{""}}},
			}, "workerPoolUpgrade stage \"canary\" contains an empty pool name"),
			Entry("master pool in a stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Name: "canary", Pools: []string{"master"}}},
			}, "workerPoolUpgrade stage must not include \"master\""),
			Entry("pool repeated between control plane and stage", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"worker-a"},
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
				},
			}, "workerPoolUpgrade pool \"worker-a\" appears more than once (poolsWithControlPlane and stage \"canary\")"),
			Entry("pool repeated within one stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a", "worker-a"}},
				},
			}, "workerPoolUpgrade pool \"worker-a\" appears more than once in stage \"canary\""),
			Entry("pool repeated across stages", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
					{Name: "rest", Pools: []string{"worker-a"}},
				},
			}, "workerPoolUpgrade pool \"worker-a\" appears more than once (stage \"canary\" and stage \"rest\")"),
			Entry("duplicate control-plane pool", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"worker-a", "worker-a"},
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-b"}},
				},
			}, "workerPoolUpgrade.poolsWithControlPlane contains duplicate pool name \"worker-a\""),
			Entry("empty control-plane pool name", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{""},
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-b"}},
				},
			}, "workerPoolUpgrade.poolsWithControlPlane contains an empty pool name"),
			Entry("master pool with control plane", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"master"},
				Stages:                []WorkerPoolUpgradeStage{{Name: "canary", Pools: []string{"worker-b"}}},
			}, "workerPoolUpgrade.poolsWithControlPlane must not include \"master\""),
			Entry("upgradeThrough names a missing stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
				},
				UpgradeThrough: "missing",
			}, "workerPoolUpgrade.upgradeThrough refers to unknown stage \"missing\""),
		)

		DescribeTable("rejects Custom-only fields on preset strategies",
			func(strategy string, useStages bool) {
				config := WorkerPoolUpgrade{Strategy: strategy}
				if useStages {
					config.Stages = []WorkerPoolUpgradeStage{{Name: "canary", Pools: []string{"worker-a"}}}
				} else {
					config.UpgradeThrough = "canary"
				}
				Expect(ValidateWorkerPoolUpgrade(false, config)).To(MatchError(
					"workerPoolUpgrade.stages and workerPoolUpgrade.upgradeThrough are supported only with strategy Custom"))
			},
			Entry("OpenShiftDefault with stages", constants.WorkerPoolUpgradeStrategyOpenShiftDefault, true),
			Entry("OpenShiftDefault with upgradeThrough", constants.WorkerPoolUpgradeStrategyOpenShiftDefault, false),
			Entry("Serial with stages", constants.WorkerPoolUpgradeStrategySerial, true),
			Entry("Serial with upgradeThrough", constants.WorkerPoolUpgradeStrategySerial, false),
			Entry("Parallel with stages", constants.WorkerPoolUpgradeStrategyParallel, true),
			Entry("Parallel with upgradeThrough", constants.WorkerPoolUpgradeStrategyParallel, false),
		)
	})

	Describe("ValidateWorkerPoolUpgradeMCPs", func() {
		mcps := []mcfgv1.MachineConfigPool{
			{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "worker-b"}},
		}

		It("should require every worker in the initial Custom plan", func() {
			config := WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
				},
			}
			err := ValidateWorkerPoolUpgradeMCPs(mcps, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("worker-b"))
		})

		It("should accept every worker exactly once", func() {
			config := WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"worker-a"},
				Stages: []WorkerPoolUpgradeStage{
					{Name: "rest", Pools: []string{"worker-b"}},
				},
			}
			Expect(ValidateWorkerPoolUpgradeMCPs(mcps, config)).To(Succeed())
		})

		DescribeTable("accepts known control-plane pools for preset strategies",
			func(strategy string) {
				config := WorkerPoolUpgrade{
					Strategy:              strategy,
					PoolsWithControlPlane: []string{"worker-a"},
				}
				Expect(ValidateWorkerPoolUpgradeMCPs(mcps, config)).To(Succeed())
			},
			Entry("Serial", constants.WorkerPoolUpgradeStrategySerial),
			Entry("Parallel", constants.WorkerPoolUpgradeStrategyParallel),
		)

		DescribeTable("rejects unknown poolsWithControlPlane MCPs",
			func(strategy string) {
				config := WorkerPoolUpgrade{
					Strategy:              strategy,
					PoolsWithControlPlane: []string{"missing"},
				}
				if strategy == constants.WorkerPoolUpgradeStrategyCustom {
					config.Stages = []WorkerPoolUpgradeStage{{Name: "rest", Pools: []string{"worker-a", "worker-b"}}}
				}
				Expect(ValidateWorkerPoolUpgradeMCPs(mcps, config)).To(MatchError(
					"workerPoolUpgrade refers to unknown MachineConfigPool \"missing\""))
			},
			Entry("Serial", constants.WorkerPoolUpgradeStrategySerial),
			Entry("Parallel", constants.WorkerPoolUpgradeStrategyParallel),
			Entry("Custom", constants.WorkerPoolUpgradeStrategyCustom),
		)

		It("rejects an unknown MCP in a Custom stage", func() {
			config := WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"worker-a"},
				Stages:                []WorkerPoolUpgradeStage{{Name: "rest", Pools: []string{"missing"}}},
			}
			Expect(ValidateWorkerPoolUpgradeMCPs(mcps, config)).To(MatchError(
				"workerPoolUpgrade refers to unknown MachineConfigPool \"missing\""))
		})
	})
})

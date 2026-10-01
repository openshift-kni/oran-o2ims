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

	It("accepts input without clusterVersion", func() {
		Expect(ValidateCVUpgradeData(map[string]any{"someOtherKey": "value"}, release, label)).ToNot(HaveOccurred())
	})

	It("accepts nested ClusterVersion settings", func() {
		data := map[string]any{
			"clusterVersion": map[string]any{
				"cvSpec": map[string]any{
					"desiredUpdate": map[string]any{"version": release},
				},
				"clusterUpgradeTimeout": "2h30m",
				"intermediateVersion":   "4.16.3",
				"workerPoolUpgrade":     map[string]any{"strategy": "Serial"},
			},
		}
		Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
	})

	It("allows a partial override with only nested timeout", func() {
		data := map[string]any{"clusterVersion": map[string]any{"clusterUpgradeTimeout": "2h"}}
		Expect(ValidateCVUpgradeData(data, release, "upgradeParameters")).ToNot(HaveOccurred())
	})

	DescribeTable("rejects a non-object clusterVersion with an input error",
		func(value any, contextLabel string) {
			err := ValidateCVUpgradeData(map[string]any{"clusterVersion": value}, release, contextLabel)
			Expect(err).To(HaveOccurred())
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring(contextLabel + ` "clusterVersion" value must be an object`))
		},
		Entry("string in defaults", "invalid", label),
		Entry("nil in parameters", nil, "upgradeParameters"),
	)

	DescribeTable("rejects a non-object cvSpec with an input error",
		func(value any) {
			err := ValidateCVUpgradeData(map[string]any{
				"clusterVersion": map[string]any{"cvSpec": value},
			}, release, label)
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("upgradeDefaults.clusterVersion.cvSpec must be an object")))
		},
		Entry("string", "invalid"),
		Entry("nil", nil),
		Entry("array", []any{}),
	)

	DescribeTable("accepts optional desiredUpdate fields",
		func(cvConfig map[string]any) {
			Expect(ValidateCVUpgradeData(map[string]any{"clusterVersion": cvConfig}, release, label)).ToNot(HaveOccurred())
		},
		Entry("cvSpec absent", map[string]any{}),
		Entry("desiredUpdate absent", map[string]any{"cvSpec": map[string]any{}}),
		Entry("version absent", map[string]any{"cvSpec": map[string]any{"desiredUpdate": map[string]any{}}}),
		Entry("other desiredUpdate fields without version", map[string]any{"cvSpec": map[string]any{
			"desiredUpdate": map[string]any{"image": "quay.io/example/update:4.17"},
		}}),
		Entry("version empty", map[string]any{"cvSpec": map[string]any{
			"desiredUpdate": map[string]any{"version": ""},
		}}),
	)

	It("rejects a desiredUpdate version that differs from the release", func() {
		err := ValidateCVUpgradeData(map[string]any{
			"clusterVersion": map[string]any{
				"cvSpec": map[string]any{"desiredUpdate": map[string]any{"version": "4.18.0"}},
			},
		}, release, label)
		Expect(err).To(MatchError(ContainSubstring("clusterVersion.cvSpec.desiredUpdate.version")))
		Expect(err.Error()).To(ContainSubstring("does not match the ClusterTemplate spec.release"))
	})

	DescribeTable("rejects invalid nested timeouts",
		func(value, message string) {
			err := ValidateCVUpgradeData(map[string]any{
				"clusterVersion": map[string]any{"clusterUpgradeTimeout": value},
			}, release, label)
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("clusterVersion.clusterUpgradeTimeout"))
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("invalid duration", "notaduration", "invalid clusterVersion.clusterUpgradeTimeout"),
		Entry("zero duration", "0s", "must be a positive duration"),
		Entry("negative duration", "-5m", "must be a positive duration"),
	)

	DescribeTable("validates nested intermediate versions",
		func(version, message string) {
			err := ValidateCVUpgradeData(map[string]any{
				"clusterVersion": map[string]any{"intermediateVersion": version},
			}, release, label)
			Expect(typederrors.IsInputError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("clusterVersion.intermediateVersion"))
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("invalid semver", "not-semver", "is not valid semver"),
		Entry("wrong major", "3.16.0", "major version"),
		Entry("wrong minor", "4.15.0", "exactly one minor version below"),
	)

	It("accepts an empty intermediateVersion", func() {
		data := map[string]any{"clusterVersion": map[string]any{"intermediateVersion": ""}}
		Expect(ValidateCVUpgradeData(data, release, label)).ToNot(HaveOccurred())
	})

	It("rejects an invalid release when validating intermediateVersion", func() {
		data := map[string]any{"clusterVersion": map[string]any{"intermediateVersion": "4.16.0"}}
		err := ValidateCVUpgradeData(data, "not-semver", label)
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("spec.release")))
	})

	It("reports the first semantic error", func() {
		data := map[string]any{
			"clusterVersion": map[string]any{
				"cvSpec":                map[string]any{"desiredUpdate": map[string]any{"version": "4.18.0"}},
				"clusterUpgradeTimeout": "invalid",
			},
		}
		err := ValidateCVUpgradeData(data, release, label)
		Expect(err).To(MatchError(ContainSubstring("clusterVersion.cvSpec.desiredUpdate.version")))
		Expect(err.Error()).To(ContainSubstring("does not match the ClusterTemplate spec.release"))
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
		Expect(err.Error()).To(ContainSubstring("clusterVersion.intermediateVersion"))
		Expect(err.Error()).To(ContainSubstring("exactly one minor version below"))
	})

	It("should reject wrong major version", func() {
		err := ValidateEUSIntermediate("99.21.0", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("clusterVersion.intermediateVersion"))
		Expect(err.Error()).To(ContainSubstring("must equal ClusterTemplate's spec.release major version"))
	})

	It("should reject invalid intermediateVersion", func() {
		err := ValidateEUSIntermediate("invalid", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("clusterVersion.intermediateVersion"))
		Expect(err.Error()).To(ContainSubstring("is not valid semver"))
	})

	It("should reject invalid target version", func() {
		err := ValidateEUSIntermediate("4.21.0", "not-semver")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("clusterVersion.intermediateVersion"))
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
			Expect(err.Error()).To(ContainSubstring("unsupported clusterVersion.workerPoolUpgrade.strategy"))
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
			}, "clusterVersion.workerPoolUpgrade.stages must not be empty with strategy Custom"),
			Entry("empty stage name", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Pools: []string{"worker-a"}}},
			}, "clusterVersion.workerPoolUpgrade.stages contains an empty stage name"),
			Entry("duplicate stage name", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "same", Pools: []string{"worker-a"}},
					{Name: "same", Pools: []string{"worker-b"}},
				},
			}, "clusterVersion.workerPoolUpgrade.stages contains duplicate stage name \"same\""),
			Entry("stage with no pools", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Name: "canary"}},
			}, "clusterVersion.workerPoolUpgrade stage \"canary\" must contain at least one pool"),
			Entry("empty pool name in a stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Name: "canary", Pools: []string{""}}},
			}, "clusterVersion.workerPoolUpgrade stage \"canary\" contains an empty pool name"),
			Entry("master pool in a stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages:   []WorkerPoolUpgradeStage{{Name: "canary", Pools: []string{"master"}}},
			}, "clusterVersion.workerPoolUpgrade stage must not include \"master\""),
			Entry("pool repeated between control plane and stage", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"worker-a"},
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
				},
			}, "clusterVersion.workerPoolUpgrade pool \"worker-a\" appears more than once (poolsWithControlPlane and stage \"canary\")"),
			Entry("pool repeated within one stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a", "worker-a"}},
				},
			}, "clusterVersion.workerPoolUpgrade pool \"worker-a\" appears more than once in stage \"canary\""),
			Entry("pool repeated across stages", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
					{Name: "rest", Pools: []string{"worker-a"}},
				},
			}, "clusterVersion.workerPoolUpgrade pool \"worker-a\" appears more than once (stage \"canary\" and stage \"rest\")"),
			Entry("duplicate control-plane pool", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"worker-a", "worker-a"},
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-b"}},
				},
			}, "clusterVersion.workerPoolUpgrade.poolsWithControlPlane contains duplicate pool name \"worker-a\""),
			Entry("empty control-plane pool name", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{""},
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-b"}},
				},
			}, "clusterVersion.workerPoolUpgrade.poolsWithControlPlane contains an empty pool name"),
			Entry("master pool with control plane", WorkerPoolUpgrade{
				Strategy:              constants.WorkerPoolUpgradeStrategyCustom,
				PoolsWithControlPlane: []string{"master"},
				Stages:                []WorkerPoolUpgradeStage{{Name: "canary", Pools: []string{"worker-b"}}},
			}, "clusterVersion.workerPoolUpgrade.poolsWithControlPlane must not include \"master\""),
			Entry("upgradeThrough names a missing stage", WorkerPoolUpgrade{
				Strategy: constants.WorkerPoolUpgradeStrategyCustom,
				Stages: []WorkerPoolUpgradeStage{
					{Name: "canary", Pools: []string{"worker-a"}},
				},
				UpgradeThrough: "missing",
			}, "clusterVersion.workerPoolUpgrade.upgradeThrough refers to unknown stage \"missing\""),
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
					"clusterVersion.workerPoolUpgrade.stages and clusterVersion.workerPoolUpgrade.upgradeThrough are supported only with strategy Custom"))
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
					"clusterVersion.workerPoolUpgrade refers to unknown MachineConfigPool \"missing\""))
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
				"clusterVersion.workerPoolUpgrade refers to unknown MachineConfigPool \"missing\""))
		})
	})
})

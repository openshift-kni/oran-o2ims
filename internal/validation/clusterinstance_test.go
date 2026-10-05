/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

var testClusterInstanceData = map[string]interface{}{
	"clusterName":            "site-sno-du-1",
	"baseDomain":             "example.com",
	"clusterImageSetNameRef": "4.16",
	"pullSecretRef":          map[string]interface{}{"name": "pullSecretName"},
	"templateRefs":           []map[string]interface{}{{"name": "aci-cluster-crs-v1", "namespace": "siteconfig-system"}},
	"additionalNTPSources":   []string{"NTP.server1", "1.1.1.1"},
	"apiVIPs":                []string{"192.0.2.2", "192.0.2.3"},
	"caBundleRef":            map[string]interface{}{"name": "my-bundle-ref"},
	"extraLabels":            map[string]map[string]string{"ManagedCluster": {"cluster-version": "v4.16", "clustertemplate-a-policy": "v1"}},
	"extraAnnotations":       map[string]map[string]string{"ManagedCluster": {"annKey": "annValue"}},
	"clusterType":            "SNO",
	"clusterNetwork":         []map[string]interface{}{{"cidr": "203.0.113.0/24", "hostPrefix": 23}},
	"machineNetwork":         []map[string]interface{}{{"cidr": "192.0.2.0/24"}},
	"networkType":            "OVNKubernetes",
	"cpuPartitioningMode":    "AllNodes",
	"diskEncryption":         map[string]interface{}{"tang": []map[string]interface{}{{"thumbprint": "1234567890", "url": "http://198.51.100.1:7500"}}, "type": "nbde"},
	"extraManifestsRefs":     []map[string]interface{}{{"name": "foobar1"}, {"name": "foobar2"}},
	"ignitionConfigOverride": "igen",
	"installConfigOverrides": "{\"capabilities\":{\"baselineCapabilitySet\": \"None\", \"additionalEnabledCapabilities\": [ \"marketplace\", \"NodeTuning\" ] }}",
	"proxy":                  map[string]interface{}{"noProxy": "foobar"},
	"serviceNetwork":         []map[string]interface{}{{"cidr": "233.252.0.0/24"}},
	"sshPublicKey":           "ssh-rsa",
	"nodes": []map[string]interface{}{
		{
			"bmcAddress":             "idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1",
			"bmcCredentialsName":     map[string]interface{}{"name": "node1-bmc-secret"},
			"bootMACAddress":         "00:00:00:01:20:30",
			"bootMode":               "UEFI",
			"extraLabels":            map[string]map[string]string{"NMStateConfig": {"labelKey": "labelValue"}},
			"extraAnnotations":       map[string]map[string]string{"NMStateConfig": {"annKey": "annValue"}},
			"hostName":               "node1.baseDomain.com",
			"ignitionConfigOverride": "{\"ignition\": {\"version\": \"3.1.0\"}, \"storage\": {\"files\": [{\"path\": \"/etc/containers/registries.conf\", \"overwrite\": true, \"contents\": {\"source\": \"data:text/plain;base64,aGVsbG8gZnJvbSB6dHAgcG9saWN5IGdlbmVyYXRvcg==\"}}]}}",
			"installerArgs":          "[\"--append-karg\", \"nameserver=8.8.8.8\", \"-n\"]",
			"ironicInspect":          "",
			"role":                   "master",
			"rootDeviceHint":         map[string]interface{}{"hctl": "1:2:0:0"},
			"automatedCleaningMode":  "disabled",
			"templateRefs":           []map[string]interface{}{{"name": "aci-node-crs-v1", "namespace": "siteconfig-system"}},
			"nodeNetwork": map[string]interface{}{
				"config": map[string]interface{}{
					"dns-resolver": map[string]interface{}{
						"config": map[string]interface{}{
							"server": []string{"192.0.2.22"},
						},
					},
					"interfaces": []map[string]interface{}{
						{
							"ipv4": map[string]interface{}{
								"address": []map[string]interface{}{
									{"ip": "192.0.2.10", "prefix-length": 24},
									{"ip": "192.0.2.11", "prefix-length": 24},
									{"ip": "192.0.2.12", "prefix-length": 24},
								},
								"dhcp":    false,
								"enabled": true,
							},
							"ipv6": map[string]interface{}{
								"address": []map[string]interface{}{
									{"ip": "2001:db8:0:1::42", "prefix-length": 32},
									{"ip": "2001:db8:0:1::43", "prefix-length": 32},
									{"ip": "2001:db8:0:1::44", "prefix-length": 32},
								},
								"dhcp":    false,
								"enabled": true,
							},
							"name": "eno1",
							"type": "ethernet",
						},
						{
							"ipv6": map[string]interface{}{
								"address": []map[string]interface{}{
									{"ip": "2001:db8:abcd:1234::1"},
								},
								"enabled": true,
								"link-aggregation": map[string]interface{}{
									"mode": "balance-rr",
									"options": map[string]interface{}{
										"miimon": "140",
									},
									"slaves": []string{"eth0", "eth1"},
								},
								"prefix-length": 32,
							},
							"name":  "bond99",
							"state": "up",
							"type":  "bond",
						},
					},
					"routes": map[string]interface{}{
						"config": []map[string]interface{}{
							{
								"destination":        "0.0.0.0/0",
								"next-hop-address":   "192.0.2.254",
								"next-hop-interface": "eno1",
								"table":              "",
							},
						},
					},
				},
				"interfaces": []map[string]interface{}{
					{"macAddress": "00:00:00:01:20:30", "name": "eno1"},
					{"macAddress": "02:00:00:80:12:14", "name": "eth0"},
					{"macAddress": "02:00:00:80:12:15", "name": "eth1"},
				},
			},
		},
	},
}

var _ = Describe("FindClusterInstanceImmutableFieldUpdates", func() {
	var (
		oldClusterInstance *unstructured.Unstructured
		newClusterInstance *unstructured.Unstructured
	)

	BeforeEach(func() {
		// Initialize the old and new ClusterInstances
		data, err := yaml.Marshal(testClusterInstanceData)
		Expect(err).ToNot(HaveOccurred())

		oldSpec := make(map[string]any)
		newSpec := make(map[string]any)
		Expect(yaml.Unmarshal(data, &oldSpec)).To(Succeed())
		Expect(yaml.Unmarshal(data, &newSpec)).To(Succeed())

		oldClusterInstance = &unstructured.Unstructured{
			Object: map[string]any{"spec": oldSpec},
		}

		newClusterInstance = &unstructured.Unstructured{
			Object: map[string]any{"spec": newSpec},
		}
	})

	It("should return no updates when specs are identical", func() {
		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields,
			AllowedClusterInstanceFields)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedFields).To(BeEmpty())
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should detect changes in immutable cluster-level fields", func() {
		// Change an immutable field at the cluster-level
		spec := newClusterInstance.Object["spec"].(map[string]any)
		spec["baseDomain"] = "newdomain.example.com"

		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields,
			AllowedClusterInstanceFields)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedFields).To(ContainElement("baseDomain"))
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should not flag changes in allowed cluster-level fields alongside immutable fields", func() {
		// Add an allowed extra label
		spec := newClusterInstance.Object["spec"].(map[string]any)
		// Change allowed fields
		labels := spec["extraLabels"].(map[string]any)["ManagedCluster"].(map[string]any)
		labels["newLabelKey"] = "newLabelValue"
		delete(spec, "extraAnnotations")
		// Change immutable field
		spec["clusterName"] = "newName"

		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields,
			AllowedClusterInstanceFields)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedFields).To(ContainElement("clusterName"))
		Expect(len(updatedFields)).To(Equal(1))
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should detect changes in disallowed node-level fields", func() {
		// Change an immutable field in the node-level spec
		spec := newClusterInstance.Object["spec"].(map[string]any)
		node0 := spec["nodes"].([]any)[0].(map[string]any)
		node0Network := node0["nodeNetwork"].(map[string]any)["config"].(map[string]any)["dns-resolver"].(map[string]any)
		node0Network["config"].(map[string]any)["server"].([]any)[0] = "10.19.42.42"

		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields,
			AllowedClusterInstanceFields)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedFields).To(ContainElement(
			"nodes.0.nodeNetwork.config.dns-resolver.config.server.0"))
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should not flag changes in allowed node-level fields alongside immutable fields", func() {
		// Change an allowed field and an immutable field in the same node
		spec := newClusterInstance.Object["spec"].(map[string]any)
		// Change allowed field
		nodes := spec["nodes"].([]any)
		nodes[0].(map[string]any)["extraAnnotations"] = map[string]map[string]string{
			"BareMetalHost": {
				"newAnnotationKey": "newAnnotationValue",
			},
		}
		// Change immutable field
		node0 := spec["nodes"].([]any)[0].(map[string]any)
		node0Network := node0["nodeNetwork"].(map[string]any)["config"].(map[string]any)["dns-resolver"].(map[string]any)
		node0Network["config"].(map[string]any)["server"].([]any)[0] = "10.19.42.42"

		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields,
			AllowedClusterInstanceFields)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedFields).To(ContainElement(
			"nodes.0.nodeNetwork.config.dns-resolver.config.server.0"))
		Expect(len(updatedFields)).To(Equal(1))
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should not flag changes in ignored node-level fields alongside immutable fields", func() {
		// Change ignored fields
		spec := newClusterInstance.Object["spec"].(map[string]any)
		node0 := spec["nodes"].([]any)[0].(map[string]any)
		node0["bmcAddress"] = "placeholder"
		node0["bmcCredentialsName"].(map[string]any)["name"] = "myCreds"
		node0["bootMACAddress"] = "00:00:5E:00:53:AF"
		node0NetworkInterfaces := node0["nodeNetwork"].(map[string]any)["interfaces"].([]any)
		node0NetworkInterfaces[0].(map[string]any)["macAddress"] = "00:00:5E:00:53:AF"

		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields,
			AllowedClusterInstanceFields)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedFields).To(BeEmpty())
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should treat flat nodes membership changes as immutable (scaling requires nodeGroups)", func() {
		spec := newClusterInstance.Object["spec"].(map[string]any)
		nodes := spec["nodes"].([]any)
		spec["nodes"] = append(nodes, map[string]any{"hostName": "worker2"})

		updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields,
			AllowedClusterInstanceFields)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedFields).To(ContainElement("nodes.1"))
		Expect(scalingNodes).To(BeEmpty())
	})

	// nodeGroups format cases (cluster-level coverage is shared with the flat-nodes
	// cases above; drop this Context when cleaning up legacy flat nodes).
	Context("with nodeGroups format", func() {
		// Convert legacy flat nodes into a single master nodeGroup.
		toNodeGroupsSpec := func(spec map[string]any) {
			nodes, ok := spec["nodes"]
			if !ok {
				return
			}
			delete(spec, "nodes")
			spec["nodeGroups"] = []any{
				map[string]any{
					"name":  "master",
					"nodes": nodes,
				},
			}
		}

		BeforeEach(func() {
			toNodeGroupsSpec(oldClusterInstance.Object["spec"].(map[string]any))
			toNodeGroupsSpec(newClusterInstance.Object["spec"].(map[string]any))
		})

		It("should detect changes in disallowed node-level fields", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			node0 := spec["nodeGroups"].([]any)[0].(map[string]any)["nodes"].([]any)[0].(map[string]any)
			node0Network := node0["nodeNetwork"].(map[string]any)["config"].(map[string]any)["dns-resolver"].(map[string]any)
			node0Network["config"].(map[string]any)["server"].([]any)[0] = "10.19.42.42"

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(ContainElement(
				"nodeGroups.0.nodes.0.nodeNetwork.config.dns-resolver.config.server.0"))
			Expect(scalingNodes).To(BeEmpty())
		})

		It("should not flag changes in allowed node-level fields alongside immutable fields", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			node0 := spec["nodeGroups"].([]any)[0].(map[string]any)["nodes"].([]any)[0].(map[string]any)
			node0["extraAnnotations"] = map[string]map[string]string{
				"BareMetalHost": {
					"newAnnotationKey": "newAnnotationValue",
				},
			}
			node0Network := node0["nodeNetwork"].(map[string]any)["config"].(map[string]any)["dns-resolver"].(map[string]any)
			node0Network["config"].(map[string]any)["server"].([]any)[0] = "10.19.42.42"

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(ContainElement(
				"nodeGroups.0.nodes.0.nodeNetwork.config.dns-resolver.config.server.0"))
			Expect(len(updatedFields)).To(Equal(1))
			Expect(scalingNodes).To(BeEmpty())
		})

		It("should detect addition of a new node", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			group0 := spec["nodeGroups"].([]any)[0].(map[string]any)
			nodes := group0["nodes"].([]any)
			group0["nodes"] = append(nodes, map[string]any{"hostName": "worker2"})

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(ContainElement("nodeGroups.0.nodes.1"))
		})

		It("should detect deletion of a node", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			group0 := spec["nodeGroups"].([]any)[0].(map[string]any)
			group0["nodes"] = []any{}

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(ContainElement("nodeGroups.0.nodes.0"))
		})

		It("should detect node swap as scaling when hostName changes in-place", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			node0 := spec["nodeGroups"].([]any)[0].(map[string]any)["nodes"].([]any)[0].(map[string]any)
			node0["hostName"] = "replacement-worker.example.com"

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(ContainElement("nodeGroups.0.nodes.0"))
		})

		It("should not flag changes in allowed nodeGroup-level fields", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			group0 := spec["nodeGroups"].([]any)[0].(map[string]any)
			group0["extraLabels"] = map[string]any{
				"ManagedCluster": map[string]any{"site": "edge-1"},
			}
			group0["extraAnnotations"] = map[string]any{
				"ManagedCluster": map[string]any{"ann": "value"},
			}

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(BeEmpty())
		})

		It("should detect changes in disallowed nodeGroup-level fields", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			group0 := spec["nodeGroups"].([]any)[0].(map[string]any)
			group0["nodeNetwork"] = map[string]any{
				"config": map[string]any{
					"dns-resolver": map[string]any{
						"config": map[string]any{"server": []any{"8.8.8.8"}},
					},
				},
			}

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).ToNot(BeEmpty())
			Expect(updatedFields[0]).To(HavePrefix("nodeGroups.0.nodeNetwork"))
			Expect(scalingNodes).To(BeEmpty())
		})

		It("should detect addition of an entire nodeGroup as scaling", func() {
			spec := newClusterInstance.Object["spec"].(map[string]any)
			spec["nodeGroups"] = append(spec["nodeGroups"].([]any),
				map[string]any{
					"name":  "extra",
					"nodes": []any{map[string]any{"hostName": "extra-1.example.com"}},
				})

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(Equal([]string{"nodeGroups.1"}))
		})

		It("should detect removal of a nodeGroup entry as scaling", func() {
			oldSpec := oldClusterInstance.Object["spec"].(map[string]any)
			oldSpec["nodeGroups"] = append(oldSpec["nodeGroups"].([]any),
				map[string]any{
					"name":  "worker",
					"nodes": []any{map[string]any{"hostName": "worker-1.example.com"}},
				})

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(Equal([]string{"nodeGroups.1"}))
		})

		It("should detect omitting the nodes key under a nodeGroup as scaling", func() {
			oldSpec := oldClusterInstance.Object["spec"].(map[string]any)
			oldSpec["nodeGroups"] = append(oldSpec["nodeGroups"].([]any),
				map[string]any{
					"name":  "worker",
					"nodes": []any{map[string]any{"hostName": "worker-1.example.com"}, map[string]any{"hostName": "worker-2.example.com"}},
				})
			newSpec := newClusterInstance.Object["spec"].(map[string]any)
			newSpec["nodeGroups"] = append(newSpec["nodeGroups"].([]any),
				map[string]any{
					"name": "worker",
				})

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(Equal([]string{"nodeGroups.1.nodes"}))
		})

		It("should detect adding the nodes key under a nodeGroup as scaling", func() {
			oldSpec := oldClusterInstance.Object["spec"].(map[string]any)
			oldSpec["nodeGroups"] = append(oldSpec["nodeGroups"].([]any),
				map[string]any{
					"name": "worker",
				})
			newSpec := newClusterInstance.Object["spec"].(map[string]any)
			newSpec["nodeGroups"] = append(newSpec["nodeGroups"].([]any),
				map[string]any{
					"name":  "worker",
					"nodes": []any{map[string]any{"hostName": "worker-1.example.com"}},
				})

			updatedFields, scalingNodes, err := FindClusterInstanceImmutableFieldUpdates(
				oldClusterInstance.Object["spec"].(map[string]any),
				newClusterInstance.Object["spec"].(map[string]any),
				IgnoredClusterInstanceFields,
				AllowedClusterInstanceFields)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedFields).To(BeEmpty())
			Expect(scalingNodes).To(Equal([]string{"nodeGroups.1.nodes"}))
		})
	})
})

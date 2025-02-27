package utils

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
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

var _ = Describe("RenderTemplateForK8sCR", func() {
	var (
		clusterInstanceObj   map[string]interface{}
		expectedRenderedYaml string
	)

	BeforeEach(func() {
		data, err := yaml.Marshal(testClusterInstanceData)
		Expect(err).ToNot(HaveOccurred())

		// New var to store cluster data
		clusterData := make(map[string]any)
		Expect(yaml.Unmarshal(data, &clusterData)).To(Succeed())
		clusterInstanceObj = map[string]interface{}{"Cluster": clusterData}

		expectedRenderedYaml = `
apiVersion: siteconfig.open-cluster-management.io/v1alpha1
kind: ClusterInstance
metadata:
  name: site-sno-du-1
  namespace: site-sno-du-1
spec:
  additionalNTPSources:
  - NTP.server1
  - 1.1.1.1
  apiVIPs:
  - 192.0.2.2
  - 192.0.2.3
  baseDomain: example.com
  caBundleRef:
    name: my-bundle-ref
  clusterImageSetNameRef: "4.16"
  extraLabels:
    ManagedCluster:
      cluster-version: v4.16
      clustertemplate-a-policy: v1
  extraAnnotations:
    ManagedCluster:
      annKey: annValue
  clusterName: site-sno-du-1
  clusterNetwork:
  - cidr: 203.0.113.0/24
    hostPrefix: 23
  clusterType: SNO
  cpuPartitioningMode: AllNodes
  diskEncryption:
    tang:
    - thumbprint: "1234567890"
      url: http://198.51.100.1:7500
    type: nbde
  extraManifestsRefs:
  - name: foobar1
  - name: foobar2
  holdInstallation: false
  ignitionConfigOverride: igen
  installConfigOverrides: '{"capabilities":{"baselineCapabilitySet": "None", "additionalEnabledCapabilities":
    [ "marketplace", "NodeTuning" ] }}'
  machineNetwork:
  - cidr: 192.0.2.0/24
  networkType: OVNKubernetes
  nodes:
  - automatedCleaningMode: disabled
    bmcAddress: idrac-virtualmedia+https://203.0.113.5/redfish/v1/Systems/System.Embedded.1
    bmcCredentialsName:
      name: node1-bmc-secret
    bootMACAddress: "00:00:00:01:20:30"
    bootMode: UEFI
    extraLabels:
      NMStateConfig:
        labelKey: labelValue
    extraAnnotations:
      NMStateConfig:
        annKey: annValue
    hostName: node1.baseDomain.com
    ignitionConfigOverride: '{"ignition": {"version": "3.1.0"}, "storage": {"files":
      [{"path": "/etc/containers/registries.conf", "overwrite": true, "contents":
      {"source": "data:text/plain;base64,aGVsbG8gZnJvbSB6dHAgcG9saWN5IGdlbmVyYXRvcg=="}}]}}'
    installerArgs: '["--append-karg", "nameserver=8.8.8.8", "-n"]'
    ironicInspect: ""
    nodeNetwork:
      config:
        dns-resolver:
          config:
            server:
            - 192.0.2.22
        interfaces:
        - ipv4:
            address:
            - ip: 192.0.2.10
              prefix-length: 24
            - ip: 192.0.2.11
              prefix-length: 24
            - ip: 192.0.2.12
              prefix-length: 24
            dhcp: false
            enabled: true
          ipv6:
            address:
            - ip: 2001:db8:0:1::42
              prefix-length: 32
            - ip: 2001:db8:0:1::43
              prefix-length: 32
            - ip: 2001:db8:0:1::44
              prefix-length: 32
            dhcp: false
            enabled: true
          name: eno1
          type: ethernet
        - ipv6:
            address:
            - ip: 2001:db8:abcd:1234::1
            enabled: true
            link-aggregation:
              mode: balance-rr
              options:
                miimon: "140"
              slaves:
              - eth0
              - eth1
            prefix-length: 32
          name: bond99
          state: up
          type: bond
        routes:
          config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.0.2.254
            next-hop-interface: eno1
            table: ""
      interfaces:
      - macAddress: 00:00:00:01:20:30
        name: eno1
      - macAddress: 02:00:00:80:12:14
        name: eth0
      - macAddress: 02:00:00:80:12:15
        name: eth1
    role: master
    templateRefs:
    - name: aci-node-crs-v1
      namespace: siteconfig-system
  proxy:
    noProxy: foobar
  pullSecretRef:
    name: pullSecretName
  serviceNetwork:
  - cidr: 233.252.0.0/24
  sshPublicKey: ssh-rsa
  templateRefs:
  - name: aci-cluster-crs-v1
    namespace: siteconfig-system
    `
	})

	It("Renders the cluster instance template successfully", func() {
		expectedRenderedClusterInstance := &unstructured.Unstructured{}
		err := yaml.Unmarshal([]byte(expectedRenderedYaml), expectedRenderedClusterInstance)
		Expect(err).ToNot(HaveOccurred())

		renderedClusterInstance, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).ToNot(HaveOccurred())

		yamlString, err := yaml.Marshal(renderedClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		fmt.Println(string(yamlString))

		if !reflect.DeepEqual(renderedClusterInstance, expectedRenderedClusterInstance) {
			err = fmt.Errorf("renderedClusterInstance not equal, expected = %v, got = %v",
				renderedClusterInstance, expectedRenderedClusterInstance)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("Renders the cluster instance template with default bmcCredentialsName", func() {
		// Remove the bmcCredentialsName
		node1 := clusterInstanceObj["Cluster"].(map[string]any)["nodes"].([]any)[0]
		delete(node1.(map[string]any), "bmcCredentialsName")

		expectedRenderedClusterInstance := &unstructured.Unstructured{}
		err := yaml.Unmarshal([]byte(expectedRenderedYaml), expectedRenderedClusterInstance)
		Expect(err).ToNot(HaveOccurred())

		renderedClusterInstance, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).ToNot(HaveOccurred())

		yamlString, err := yaml.Marshal(renderedClusterInstance)
		Expect(err).ToNot(HaveOccurred())
		fmt.Println(string(yamlString))

		if !reflect.DeepEqual(renderedClusterInstance, expectedRenderedClusterInstance) {
			err = fmt.Errorf("renderedClusterInstance not equal, expected = %v, got = %v",
				renderedClusterInstance, expectedRenderedClusterInstance)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("Return error if a required string field is empty", func() {
		// Update the required field baseDomain to empty string
		clusterInstanceObj["Cluster"].(map[string]any)["baseDomain"] = ""
		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.baseDomain cannot be empty"))
	})

	It("Return error if a required array field is empty", func() {
		// Update the required field templateRefs to empty slice
		clusterInstanceObj["Cluster"].(map[string]any)["templateRefs"] = []string{}
		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.templateRefs cannot be empty"))
	})

	It("Return error if a required map field is empty", func() {
		// Update the required field pullSecretRef to empty map
		clusterInstanceObj["Cluster"].(map[string]any)["pullSecretRef"] = map[string]any{}
		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.pullSecretRef cannot be empty"))
	})

	It("Return error if a required field is not provided", func() {
		// Remove the required field hostName
		node1 := clusterInstanceObj["Cluster"].(map[string]any)["nodes"].([]any)[0]
		delete(node1.(map[string]any), "hostName")

		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodes[0].hostName must be provided"))
	})

	It("Return error if expected array field is not an array", func() {
		// Change the nodes.nodeNetwork.interfaces to a map
		node1 := clusterInstanceObj["Cluster"].(map[string]any)["nodes"].([]any)[0]
		delete(node1.(map[string]any)["nodeNetwork"].(map[string]any), "interfaces")
		node1.(map[string]any)["nodeNetwork"].(map[string]any)["interfaces"] = map[string]any{"macAddress": "00:00:00:01:20:30", "name": "eno1"}

		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodes[0].nodeNetwork.interfaces must be of type array"))
	})

	It("Return error if expected map field is not a map", func() {
		// Change the nodes.nodeNetwork to string
		node1 := clusterInstanceObj["Cluster"].(map[string]any)["nodes"].([]any)[0]
		delete(node1.(map[string]any), "nodeNetwork")
		node1.(map[string]any)["nodeNetwork"] = "string"

		_, err := RenderTemplateForK8sCR(
			ClusterInstanceTemplateName, ClusterInstanceTemplatePath, clusterInstanceObj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.nodes[0].nodeNetwork must be of type map"))
	})
})

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
		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
		Expect(updatedFields).To(BeEmpty())
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should detect changes in immutable cluster-level fields", func() {
		// Change an immutable field at the cluster-level
		spec := newClusterInstance.Object["spec"].(map[string]any)
		spec["baseDomain"] = "newdomain.example.com"

		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
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

		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
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

		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
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

		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
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

		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
		Expect(updatedFields).To(BeEmpty())
		Expect(scalingNodes).To(BeEmpty())
	})

	It("should detect addition of a new node", func() {
		// Add a new node
		spec := newClusterInstance.Object["spec"].(map[string]any)
		nodes := spec["nodes"].([]any)
		nodes = append(nodes, map[string]any{"hostName": "worker2"})
		spec["nodes"] = nodes

		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
		Expect(updatedFields).To(BeEmpty())
		Expect(scalingNodes).To(ContainElement("nodes.1"))
	})

	It("should detect deletion of a node", func() {
		// Remove the node
		spec := newClusterInstance.Object["spec"].(map[string]any)
		spec["nodes"] = []any{}

		updatedFields, scalingNodes, err := provisioningv1alpha1.FindClusterInstanceImmutableFieldUpdates(
			oldClusterInstance.Object["spec"].(map[string]any),
			newClusterInstance.Object["spec"].(map[string]any),
			IgnoredClusterInstanceFields)
		Expect(err).To(BeNil())
		Expect(updatedFields).To(BeEmpty())
		Expect(scalingNodes).To(ContainElement("nodes.0"))
	})
})

var _ = Describe("ClusterIsReadyForPolicyConfig", func() {
	var (
		ctx         context.Context
		fakeClient  client.Client
		clusterName = "cluster-1"
	)

	suitescheme.AddKnownTypes(clusterv1.SchemeGroupVersion, &clusterv1.ManagedCluster{})
	suitescheme.AddKnownTypes(clusterv1.SchemeGroupVersion, &clusterv1.ManagedClusterList{})

	BeforeEach(func() {
		// Define the needed resources.
		crs := []client.Object{
			// Managed clusters
			&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Spec: clusterv1.ManagedClusterSpec{
					HubAcceptsClient: true,
				},
				Status: clusterv1.ManagedClusterStatus{},
			},
		}

		fakeClient = getFakeClientFromObjects(crs...)
	})

	It("returns false and no error if the cluster doesn't exist", func() {
		isReadyForConfig, err := ClusterIsReadyForPolicyConfig(ctx, fakeClient, "randomName")
		Expect(err).ToNot(HaveOccurred())
		Expect(isReadyForConfig).To(BeFalse())
	})

	It("returns false if cluster is either not available, hubAccepted or has not joined", func() {
		// Update the managedCluster cluster-1 to be available, joined and accepted.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := DoesK8SResourceExist(
			ctx, fakeClient, clusterName, "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionFalse,
			"Managed cluster is available",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionHubAccepted),
			"HubClusterAdminAccepted",
			metav1.ConditionTrue,
			"Accepted by hub cluster admin",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionJoined),
			"ManagedClusterJoined",
			metav1.ConditionTrue,
			"Managed cluster joined",
		)
		err = CreateK8sCR(context.TODO(), fakeClient, managedCluster1, nil, UPDATE)
		Expect(err).ToNot(HaveOccurred())

		isReadyForConfig, err := ClusterIsReadyForPolicyConfig(ctx, fakeClient, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(isReadyForConfig).To(BeFalse())
	})

	It("returns true if cluster is available, hubAccepted and has joined", func() {
		// Update the managedCluster cluster-1 to be available, joined and accepted.
		managedCluster1 := &clusterv1.ManagedCluster{}
		managedClusterExists, err := DoesK8SResourceExist(
			ctx, fakeClient, clusterName, "", managedCluster1)
		Expect(err).ToNot(HaveOccurred())
		Expect(managedClusterExists).To(BeTrue())
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionAvailable),
			"ManagedClusterAvailable",
			metav1.ConditionTrue,
			"Managed cluster is available",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionHubAccepted),
			"HubClusterAdminAccepted",
			metav1.ConditionTrue,
			"Accepted by hub cluster admin",
		)
		SetStatusCondition(&managedCluster1.Status.Conditions,
			provisioningv1alpha1.ConditionType(clusterv1.ManagedClusterConditionJoined),
			"ManagedClusterJoined",
			metav1.ConditionTrue,
			"Managed cluster joined",
		)
		err = CreateK8sCR(context.TODO(), fakeClient, managedCluster1, nil, UPDATE)
		Expect(err).ToNot(HaveOccurred())

		isReadyForConfig, err := ClusterIsReadyForPolicyConfig(ctx, fakeClient, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(isReadyForConfig).To(BeTrue())
	})
})

var _ = Describe("removeLabelFromInterfaces", func() {

	It("returns error for invalid node structure", func() {
		data := map[string]interface{}{
			"baseDomain": "example.sno",
			"nodes": []interface{}{
				42, // should be a map
			},
		}
		err := removeLabelFromInterfaces(data)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("unexpected: invalid node data structure"))
	})

	It("returns error for failing to extract node interfaces", func() {
		data := map[string]interface{}{
			"baseDomain": "example.sno",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": "value", // should be a map
				},
			},
		}
		err := removeLabelFromInterfaces(data)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("failed to extract the interfaces from the node map"))
	})

	It("removes the labels from the  node interfaces", func() {
		data := map[string]interface{}{
			"baseDomain": "example.sno",
			"nodes": []interface{}{
				map[string]interface{}{
					"nodeNetwork": map[string]interface{}{
						"interfaces": []interface{}{
							map[string]interface{}{
								"name":       "eth0",
								"label":      "boot-interface",
								"macAddress": "some-mac",
							},
						},
					},
				},
			},
		}
		err := removeLabelFromInterfaces(data)
		Expect(err).To(Not(HaveOccurred()))
		Expect(
			data["nodes"].([]interface{})[0].(map[string]interface{})["nodeNetwork"].(map[string]interface{})["interfaces"].([]interface{})[0].(map[string]interface{})).
			To(Equal(
				map[string]interface{}{
					"name":       "eth0",
					"macAddress": "some-mac",
				}),
			)
	})
})

var _ = Describe("removeRequiredFromClusterInstanceSchema", func() {

	It("removes all the requested attributes from maps and arrays", func() {
		var specIntf map[string]interface{}
		err := yaml.Unmarshal([]byte(TestClusterInstancePropertiesRequiredRemoval), &specIntf)
		Expect(err).ToNot(HaveOccurred())
		Expect(specIntf).To(HaveKey("required"))
		Expect(
			specIntf["properties"].(map[string]interface{})["machineNetwork"].(map[string]interface{})["items"].(map[string]interface{})).To(HaveKey("required"))
		removeRequiredFromSchema(specIntf)
		Expect(specIntf).To(Not(HaveKey("required")))
		Expect(
			specIntf["properties"].(map[string]interface{})["machineNetwork"].(map[string]interface{})["items"].(map[string]interface{})).To(Not(HaveKey("required")))
		nodeItems := specIntf["properties"].(map[string]interface{})["nodes"].(map[string]interface{})["items"].(map[string]interface{})
		nodeNetworkProperties := nodeItems["properties"].(map[string]interface{})["nodeNetwork"].(map[string]interface{})["properties"].(map[string]interface{})
		interfacesItems := nodeNetworkProperties["interfaces"].(map[string]interface{})["items"].(map[string]interface{})
		Expect(interfacesItems).To(Not(HaveKey("required")))
	})
})

var _ = Describe("ValidateDefaultConfigmapSchema", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
	)

	BeforeEach(func() {
		fakeClient = getFakeClientFromObjects()
	})

	It("returns an error when the ClusterInstance CRD does not exist", func() {
		var data map[string]interface{}
		err := ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, fakeClient, data)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).
			To(Equal(
				fmt.Sprintf(
					"failed to obtain the %s.%s CRD: customresourcedefinitions.apiextensions.k8s.io \"%s.%s\" not found",
					ClusterInstanceCrdName, siteconfig.Group, ClusterInstanceCrdName, siteconfig.Group)))
	})

	Context("The ClusterInstance CRD does exists", func() {
		It("returns error if the ClusterInstance CRD is missing its versions", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecNoVersions)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			var data map[string]interface{}
			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, fakeClient, data)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("failed to obtain the versions of the %s.%s CRD", ClusterInstanceCrdName, siteconfig.Group)))
		})

		It("returns error if no version of the ClusterInstance CRD is served", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecServedFalse)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			var data map[string]interface{}
			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, fakeClient, data)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("no version served & stored in the %s.%s CRD ", ClusterInstanceCrdName, siteconfig.Group)))
		})

		It("returns error if the ConfigMap schema does not match the ClusterInstance CRD schema", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecOk)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			data := map[string]interface{}{
				"clusterImageSetNameRef": "4.15",
				"pullSecretRef": map[string]interface{}{
					"should-be-name": "pull-secret",
				},
			}
			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, fakeClient, data)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(
				"ConfigMap does not match the ClusterInstance schema: " +
					"invalid input: pullSecretRef: Additional property should-be-name is not allowed"))
		})

		It("returns no error if the ConfigMap schema matches the ClusterInstance CRD schema", func() {
			clusterInstanceCRD, err := BuildTestClusterInstanceCRD(TestClusterInstanceSpecOk)
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeClient.Create(ctx, clusterInstanceCRD)).To(Succeed())

			data := map[string]interface{}{
				"clusterImageSetNameRef": "4.15",
				"pullSecretRef": map[string]interface{}{
					"name": "pull-secret",
				},
			}
			err = ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, fakeClient, data)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func TestExtractSchemaRequired(t *testing.T) {
	type args struct {
		mainSchema []byte
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "ok",
			args: args{
				mainSchema: []byte(`{
					"required": [
					  "nodeClusterName",
					  "oCloudSiteId",
					  "policyTemplateParameters",
					  "clusterInstanceParameters"
					]
				  }`),
			},
			want:    []string{"nodeClusterName", "oCloudSiteId", "policyTemplateParameters", "clusterInstanceParameters"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractSchemaRequired(tt.args.mainSchema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractSchemaRequired() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractSchemaRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

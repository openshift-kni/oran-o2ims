/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package teste2eutils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oranhwmgrplugintestutils "github.com/openshift-kni/oran-hwmgr-plugin/test/utils"
	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

func RemoveRequiredFieldFromClusterInstanceCm(
	ctx context.Context, c client.Client, cmName, cmNamespace string) {
	// Remove a required field from ClusterInstance default configmap
	ciConfigmap := &corev1.ConfigMap{}
	Expect(c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cmNamespace}, ciConfigmap)).To(Succeed())

	ciConfigmap.Data = map[string]string{
		utils.ClusterInstanceTemplateDefaultsConfigmapKey: `
    clusterImageSetNameRef: "4.15"
    pullSecretRef:
      name: "pull-secret"
    nodes:
    - hostName: "node1"
      role: master
      bootMode: UEFI
      nodeNetwork:
        interfaces:
        - name: eno1
          label: bootable-interface
        - name: eth0
          label: base-interface
        - name: eth1
          label: data-interface
    templateRefs:
    - name: "ai-cluster-templates-v1"
      namespace: "siteconfig-operator"
    `}
	Expect(c.Update(ctx, ciConfigmap)).To(Succeed())
}

func RemoveRequiredFieldFromClusterInstanceInput(
	ctx context.Context, c client.Client, crName string) {
	// Remove required field hostname
	currentCR := &provisioningv1alpha1.ProvisioningRequest{}
	Expect(c.Get(ctx, types.NamespacedName{Name: crName}, currentCR)).To(Succeed())

	clusterTemplateInput := make(map[string]any)
	err := json.Unmarshal([]byte(TestFullTemplateParameters), &clusterTemplateInput)
	Expect(err).ToNot(HaveOccurred())
	node1 := clusterTemplateInput["clusterInstanceParameters"].(map[string]any)["nodes"].([]any)[0]
	delete(node1.(map[string]any), "hostName")
	updatedClusterTemplateInput, err := json.Marshal(clusterTemplateInput)
	Expect(err).ToNot(HaveOccurred())

	currentCR.Spec.TemplateParameters.Raw = updatedClusterTemplateInput
	Expect(c.Update(ctx, currentCR)).To(Succeed())
}

func VerifyStatusCondition(actualCond, expectedCon metav1.Condition) {
	Expect(actualCond.Type).To(Equal(expectedCon.Type))
	Expect(actualCond.Status).To(Equal(expectedCon.Status))
	Expect(actualCond.Reason).To(Equal(expectedCon.Reason))
	if expectedCon.Message != "" {
		Expect(actualCond.Message).To(ContainSubstring(expectedCon.Message))
	}
}

func VerifyProvisioningStatus(provStatus provisioningv1alpha1.ProvisioningStatus,
	expectedPhase provisioningv1alpha1.ProvisioningPhase, expectedDetail string,
	expectedResources *provisioningv1alpha1.ProvisionedResources) {

	Expect(provStatus.ProvisioningPhase).To(Equal(expectedPhase))
	Expect(provStatus.ProvisioningDetails).To(ContainSubstring(expectedDetail))
	Expect(provStatus.ProvisionedResources).To(Equal(expectedResources))
}

// GetExternalCrdFiles downloads the external CRDs that IMS depends on based on
// the content of the go.mod file. The files are saved in the destDir directory.
func GetExternalCrdFiles(destDir string) error {
	for _, externalCrd := range ExternalCrdData {
		// Get the commit sha from the go.mod of the IMS repo.
		policyMod := externalCrd["modName"] + "/" + externalCrd["repoName"]
		_, policyModPseudoVersionNew, err := oranhwmgrplugintestutils.GetModuleFromGoMod(policyMod)
		if err != nil {
			return fmt.Errorf("error getting module from go.mod: %w", err)
		}
		commitSha := oranhwmgrplugintestutils.GetGitCommitFromPseudoVersion(policyModPseudoVersionNew)

		// Get the full sha of the commit by calling the github API.
		url := fmt.Sprintf(GithubCommitsAPI, externalCrd["owner"], externalCrd["repoName"], commitSha)
		resp, err := http.Get(url) //nolint
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		// Check that the status is ok.
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status")
		}

		// Decode the response.
		var commit map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
			return fmt.Errorf("failed to decode")
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(commit).To(HaveKey("sha"))
		// Extract the commit sha.
		fullCommitSha := commit["sha"].(string)

		// Get the CRD file.
		crdFilePath := fmt.Sprintf(
			GithubUserContentLink,
			externalCrd["owner"], externalCrd["repoName"],
			fullCommitSha, externalCrd["crdPath"], externalCrd["crdFileName"])
		err = DownloadFile(crdFilePath, externalCrd["crdFileName"], destDir)
		Expect(err).NotTo(HaveOccurred())
	}

	return nil
}

func DownloadFile(rawUrl, filename, dirpath string) error {
	// Parse the URL
	parsedURL, err := url.Parse(rawUrl)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check that the scheme is "http" or "https" and the domain is trusted
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}

	_, err = os.Stat(filename)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error getting file: %w", err)
	}

	resp, err := http.Get(rawUrl) //nolint
	if err != nil {
		return fmt.Errorf("error getting URL: %w", err)
	}
	defer resp.Body.Close()

	filepath := filepath.Join(dirpath, filename)
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err)
	}

	return nil
}

func CreateNodeResources(ctx context.Context, c client.Client, npName string) {
	node := CreateNode(MasterNodeName, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1", "bmc-secret", "controller", utils.UnitTestHwmgrNamespace, npName, nil)
	secrets := CreateSecrets([]string{BmcSecretName}, utils.UnitTestHwmgrNamespace)
	CreateResources(ctx, c, []*hwv1alpha1.AllocatedNode{node}, secrets)
}

func CreateResources(ctx context.Context, c client.Client, nodes []*hwv1alpha1.AllocatedNode, secrets []*corev1.Secret) {
	for _, node := range nodes {
		Expect(c.Create(ctx, node)).To(Succeed())
	}
	for _, secret := range secrets {
		Expect(c.Create(ctx, secret)).To(Succeed())
	}
}

func CreateNode(name, bmcAddress, bmcSecret, groupName, namespace, narName string, interfaces []*hwv1alpha1.Interface) *hwv1alpha1.AllocatedNode {
	if interfaces == nil {
		interfaces = []*hwv1alpha1.Interface{
			{
				Name:       "eno1",
				Label:      "bootable-interface",
				MACAddress: "00:00:00:01:20:30",
			},
			{
				Name:       "eth0",
				Label:      "base-interface",
				MACAddress: "00:00:00:01:20:31",
			},
			{
				Name:       "eth1",
				Label:      "data-interface",
				MACAddress: "00:00:00:01:20:32",
			},
		}
	}
	return &hwv1alpha1.AllocatedNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: hwv1alpha1.AllocatedNodeSpec{
			NodeAllocationRequest: narName,
			GroupName:             groupName,
			HwMgrId:               utils.UnitTestHwmgrID,
			HwMgrNodeId:           name,
		},
		Status: hwv1alpha1.AllocatedNodeStatus{
			BMC: &hwv1alpha1.BMC{
				Address:         bmcAddress,
				CredentialsName: bmcSecret,
			},
			Interfaces: interfaces,
		},
	}
}

func CreateSecrets(names []string, namespace string) []*corev1.Secret {
	var secrets []*corev1.Secret
	for _, name := range names {
		secrets = append(secrets, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		})
	}
	return secrets
}

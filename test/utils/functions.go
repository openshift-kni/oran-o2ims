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
	"time"

	. "github.com/onsi/gomega"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

func RemoveRequiredFieldFromClusterInstanceCm(
	ctx context.Context, c client.Client, cmName, cmNamespace string) {
	// Remove a required field from ClusterInstance default configmap
	ciConfigmap := &corev1.ConfigMap{}
	Expect(c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cmNamespace}, ciConfigmap)).To(Succeed())

	ciConfigmap.Data = map[string]string{
		ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey: `
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
		// Use modulePath if provided, otherwise construct from modName/repoName
		var policyMod string
		if modulePath, exists := externalCrd["modulePath"]; exists && modulePath != "" {
			policyMod = modulePath
		} else {
			policyMod = externalCrd["modName"] + "/" + externalCrd["repoName"]
		}
		_, policyModPseudoVersionNew, err := GetModuleFromGoMod(policyMod)
		if err != nil {
			return fmt.Errorf("error getting module from go.mod: %w", err)
		}
		commitSha := GetGitCommitFromPseudoVersion(policyModPseudoVersionNew)

		// Get the full sha of the commit by calling the github API.
		// Retry with exponential backoff.
		url := fmt.Sprintf(GithubCommitsAPI, externalCrd["owner"], externalCrd["repoName"], commitSha)
		fullCommitSha, err := RetryWithExponentialBackoff(func() (string, error) {
			resp, err := http.Get(url) //nolint
			if err != nil {
				return "", fmt.Errorf("error getting URL: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return "", fmt.Errorf("unexpected status, got: %d", resp.StatusCode)
			}
			var commit map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
				return "", fmt.Errorf("failed to decode")
			}
			Expect(commit).To(HaveKey("sha"))
			return commit["sha"].(string), nil
		})
		Expect(err).NotTo(HaveOccurred())
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

// RetryWithExponentialBackoff retries a function with exponential backoff
func RetryWithExponentialBackoff(fn func() (string, error)) (string, error) {
	maxRetries := 3
	baseDelay := 100 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		if i == maxRetries-1 {
			return "", err
		}

		delay := baseDelay * time.Duration(1<<uint(i))
		time.Sleep(delay)
	}

	return "", fmt.Errorf("max retries exceeded")
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
	node := CreateNode(MasterNodeName, "idrac-virtualmedia+https://10.16.2.1/redfish/v1/Systems/System.Embedded.1", "bmc-secret", "controller", constants.DefaultNamespace, npName, nil)
	// Create both the standard secret and the mock server expected secrets
	secretNames := []string{BmcSecretName, "test-node-1-bmc-secret", "master-node-2-bmc-secret"}
	secrets := CreateSecrets(secretNames, constants.DefaultNamespace)
	CreateResources(ctx, c, []*pluginsv1alpha1.AllocatedNode{node}, secrets)
}

func CreateResources(ctx context.Context, c client.Client, nodes []*pluginsv1alpha1.AllocatedNode, secrets []*corev1.Secret) {
	for _, node := range nodes {
		err := c.Create(ctx, node)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}
	for _, secret := range secrets {
		err := c.Create(ctx, secret)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}
}

func CreateNode(name, bmcAddress, bmcSecret, groupName, namespace, narName string, interfaces []*pluginsv1alpha1.Interface) *pluginsv1alpha1.AllocatedNode {
	if interfaces == nil {
		interfaces = []*pluginsv1alpha1.Interface{
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
	return &pluginsv1alpha1.AllocatedNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pluginsv1alpha1.AllocatedNodeSpec{
			NodeAllocationRequest: narName,
			GroupName:             groupName,
			HardwarePluginRef:     constants.DefaultNamespace,
			HwMgrNodeId:           name,
		},
		Status: pluginsv1alpha1.AllocatedNodeStatus{
			BMC: &pluginsv1alpha1.BMC{
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

// Helper function to create BareMetalHost
func CreateBareMetalHost(bmhData struct {
	Name             string
	MacAddress       string
	BmcAddress       string
	Hostname         string
	RamMB            int32
	HwProfile        string
	Colour           string
	StorageSizeBytes metal3v1alpha1.Capacity
	IsPreferred      bool
}) *metal3v1alpha1.BareMetalHost {
	return &metal3v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmhData.Name,
			Namespace: constants.DefaultNamespace,
			Labels: map[string]string{
				"resourceselector.clcm.openshift.io/server-colour": bmhData.Colour,
				"resources.clcm.openshift.io/resourcePoolId":       TestPoolID,
				"resourceselector.clcm.openshift.io/server-type":   TestServerType,
			},
		},
		Spec: metal3v1alpha1.BareMetalHostSpec{
			Online: true,
			BMC: metal3v1alpha1.BMCDetails{
				Address:         bmhData.BmcAddress,
				CredentialsName: fmt.Sprintf("%s-bmc-secret", bmhData.Name),
			},
			BootMACAddress: bmhData.MacAddress,
		},
	}
}

// Helper function to create HardwareData CR
func CreateHardwareData(bmhName string, bmhData struct {
	Name             string
	MacAddress       string
	BmcAddress       string
	Hostname         string
	RamMB            int32
	HwProfile        string
	Colour           string
	StorageSizeBytes metal3v1alpha1.Capacity
	IsPreferred      bool
}) *metal3v1alpha1.HardwareData {
	return &metal3v1alpha1.HardwareData{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmhData.Name,
			Namespace: constants.DefaultNamespace,
		},
		Spec: metal3v1alpha1.HardwareDataSpec{
			HardwareDetails: &metal3v1alpha1.HardwareDetails{
				Hostname: bmhData.Hostname,
				CPU: metal3v1alpha1.CPU{
					Arch: "x86_64",
				},
				RAMMebibytes: int(bmhData.RamMB),
				NIC: []metal3v1alpha1.NIC{
					{
						Name: "eno1",
						MAC:  bmhData.MacAddress,
					},
					{
						Name: "eth0",
						MAC:  fmt.Sprintf("%s:01", bmhData.MacAddress[:14]),
					},
					{
						Name: "eth1",
						MAC:  fmt.Sprintf("%s:02", bmhData.MacAddress[:14]),
					},
				},
				Storage: []metal3v1alpha1.Storage{
					{
						Name:         "sda",
						SizeBytes:    bmhData.StorageSizeBytes,
						Rotational:   false,
						Type:         "SSD",
						Model:        "Samsung SSD 980 PRO 1TB",
						SerialNumber: fmt.Sprintf("SN-%s", bmhData.Name),
					},
				},
			},
		},
	}
}

// Helper function to create BMC secret
func CreateBMCSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-bmc-secret", name),
			Namespace: constants.DefaultNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("password123"),
		},
	}
}

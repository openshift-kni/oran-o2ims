package api

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OranO2ims Test Suite")
}

var _ = BeforeSuite(func() {
	Expect(os.Getenv("TEST_HOST")).NotTo(BeZero(), "Please make sure TEST_HOST is set correctly.")
	if os.Getenv("TEST_HOST") == "" {
		Skip("API tests were skipped because environment variable 'TEST_HOST' isn't set")
	}
})

var _ = Describe("Metadata Server API testing", func() {
	Context("When getting infrastructure Inventory API version", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(`{"uriPrefix":"/o2ims-infrastructureInventory/v1","apiVersions":[{"version":"1.0.0"}]}`)
			request, _ := http.NewRequest("GET", "https://"+os.Getenv("TEST_HOST")+"/o2ims-infrastructureInventory/api_versions", bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")

			By("Executing https petition")
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())

			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			By("Checking response JSON is equal to expected JSON")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJSON(requestBody))
		})
	})
	Context("When getting infrastructure Inventory description", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(`{"serviceUri":"https://` + os.Getenv("TEST_HOST") + `","extensions":{},"oCloudId":"f7fd171f-57b5-4a17-b176-9a73bf6064a4","globalCloudId":"f7fd171f-57b5-4a17-b176-9a73bf6064a4","name":"OpenShift O-Cloud","description":"OpenShift O-Cloud"}`)
			request, _ := http.NewRequest("GET", "https://"+os.Getenv("TEST_HOST")+"/o2ims-infrastructureInventory/v1", bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")

			By("Executing https petition")
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			By("Checking response JSON is equal to expected JSON")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJSON(requestBody))
		})
	})
})
var _ = Describe("Deployment Manager Server API testing", func() {
	Context("When getting Deployment managers description", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(`{"serviceUri":"","extensions":{},"oCloudId":"123","globalCloudId":"123","name":"OpenShift O-Cloud","description":"OpenShift O-Cloud"}`)
			request, _ := http.NewRequest("GET", "https://"+os.Getenv("TEST_HOST")+"/o2ims-infrastructureInventory/v1/deploymentManagers", bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")

			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJSON(requestBody))
		})
	})
})

var _ = Describe("Resources Server API testing", func() {
	Context("When getting Resource Type list", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(`[{
				"model": "",
				"name": "node_16_cores_amd64",
				"resourceKind": "PHYSICAL",
				"resourceClass": "COMPUTE",
				"alarmDictionary": {
					"alarmDictionarySchemaVersion": "",
					"entityType": "",
					"vendor": "",
					"alarmDefinition": [
						{
							"proposedRepairActions": "",
							"managementInterfaceId": "O2IMS",
							"pkNotificationField": "alarmDefinitionID",
							"alarmAdditionalFields": {
								"resourceClass": "COMPUTE"
							},
							"alarmDefinitionId": "NodeClockNotSynchronising",
							"alarmName": "Clock not synchronising.",
							"alarmDescription": "Clock on host is not synchronising. Ensure NTP is configured on this host."
						},
						{
							"alarmDescription": "Clock is out of sync by more than 0.05s. Ensure NTP is configured correctly on this host.",
							"proposedRepairActions": "",
							"managementInterfaceId": "O2IMS",
							"pkNotificationField": "alarmDefinitionID",
							"alarmAdditionalFields": {
								"resourceClass": "COMPUTE"
							},
							"alarmDefinitionId": "NodeClockSkewDetected",
							"alarmName": "Clock skew detected."
						},
						{
							"alarmAdditionalFields": {
								"resourceClass": "COMPUTE"
							},
							"alarmDefinitionId": "IngressWithoutClassName",
							"alarmName": "Ingress without IngressClassName for 1 day",
							"alarmDescription": "This alert fires when there is an Ingress with an unset IngressClassName for longer than one day.",
							"proposedRepairActions": "",
							"managementInterfaceId": "O2IMS",
							"pkNotificationField": "alarmDefinitionID"
						},
						{
							"alarmName": "Host is running out of memory.",
							"alarmDescription": "Memory is filling up, has been above memory high utilization threshold for the last 15 minutes",
							"proposedRepairActions": "",
							"managementInterfaceId": "O2IMS",
							"pkNotificationField": "alarmDefinitionID",
							"alarmAdditionalFields": {
								"resourceClass": "COMPUTE"
							},
							"alarmDefinitionId": "NodeMemoryHighUtilization"
						}
					],
					"pkNotificationField": "alarmDefinitionID",
					"managementInterfaceId": "O2IMS",
					"alarmDictionaryVersion": "v1"
				},
				"vendor": "",
				"resourceTypeID": "node_16_cores_amd64",
				"extensions": "",
				"version": ""
			}]`)
			request, _ := http.NewRequest("GET", "https://"+os.Getenv("TEST_HOST")+"/o2ims-infrastructureInventory/v1/resourceTypes", bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")

			By("Executing https petition")
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			By("Checking response JSON is equal to expected JSON")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJSON(requestBody))
		})
	})
	Context("When getting Resource Pools", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(`[{
				"oCloudID": "f7fd171f-57b5-4a17-b176-9a73bf6064a4",
				"extensions": {
					"vendor": "OpenShift",
					"feature.open-cluster-management.io/addon-application-manager": "available",
					"feature.open-cluster-management.io/addon-hypershift-addon": "available",
					"feature.open-cluster-management.io/addon-config-policy-controller": "available",
					"feature.open-cluster-management.io/addon-managed-serviceaccount": "available",
					"local-cluster": "true",
					"openshiftVersion": "4.15.0-0.nightly-2024-05-07-065351",
					"cluster.open-cluster-management.io/clusterset": "default",
					"feature.open-cluster-management.io/addon-cert-policy-controller": "available",
					"feature.open-cluster-management.io/addon-work-manager": "available",
					"name": "local-cluster",
					"openshiftVersion-major": "4",
					"cloud": "BareMetal",
					"feature.open-cluster-management.io/addon-governance-policy-framework": "available",
					"feature.open-cluster-management.io/addon-iam-policy-controller": "available",
					"openshiftVersion-major-minor": "4.15",
					"velero.io/exclude-from-backup": "true",
					"clusterID": "c79b26c9-a37f-47f0-873a-790b31a2149d",
					"feature.open-cluster-management.io/addon-cluster-proxy": "available"
				},
				"location": "",
				"description": "c79b26c9-a37f-47f0-873a-790b31a2149d",
				"globalLocationID": "",
				"resourcePoolID": "local-cluster",
				"name": "local-cluster"
			}]`)
			request, _ := http.NewRequest("GET", "https://"+os.Getenv("TEST_HOST")+"/o2ims-infrastructureInventory/v1/resourcePools", bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")

			By("Executing https petition")
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			By("Checking response JSON is equal to expected JSON")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJSON(requestBody))
		})
	})
	Context("When getting Resource List from a defined pool", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(`[
				{
					"globalAssetID": "local-cluster/031a33d2-93e4-4b0c-99ff-57742ee5e361",
					"resourceID": "7cc9cf38-8232-41be-b864-aa61af425af6",
					"resourceTypeID": "node_16_cores_amd64",
					"description": "master-0-2",
					"extensions": {
						"beta.kubernetes.io/arch": "amd64",
						"beta.kubernetes.io/os": "linux",
						"kubernetes.io/arch": "amd64",
						"node-role.kubernetes.io/control-plane": "",
						"node-role.kubernetes.io/worker": "",
						"kubernetes.io/hostname": "master-0-2",
						"kubernetes.io/os": "linux",
						"node-role.kubernetes.io/master": "",
						"node.openshift.io/os_id": "rhcos"
					},
					"resourcePoolID": "local-cluster"
				},
				{
					"resourceID": "5ebc8c2f-52ba-402b-9920-90b2424dba41",
					"resourceTypeID": "node_16_cores_amd64",
					"description": "master-0-0",
					"extensions": {
						"node.openshift.io/os_id": "rhcos",
						"node-role.kubernetes.io/master": "",
						"node-role.kubernetes.io/worker": "",
						"beta.kubernetes.io/arch": "amd64",
						"beta.kubernetes.io/os": "linux",
						"kubernetes.io/arch": "amd64",
						"kubernetes.io/hostname": "master-0-0",
						"kubernetes.io/os": "linux",
						"node-role.kubernetes.io/control-plane": ""
					},
					"resourcePoolID": "local-cluster",
					"globalAssetID": "local-cluster/3dd166de-520a-4d00-bb25-ba90cffab098"
				},
				{
					"globalAssetID": "local-cluster/ed18b542-7b57-4eed-8271-75509007c68f",
					"resourceID": "982c2b8d-818b-4dfb-8130-5622edc5bfc9",
					"resourceTypeID": "node_16_cores_amd64",
					"description": "master-0-1",
					"extensions": {
						"node-role.kubernetes.io/worker": "",
						"beta.kubernetes.io/arch": "amd64",
						"beta.kubernetes.io/os": "linux",
						"node-role.kubernetes.io/control-plane": "",
						"node-role.kubernetes.io/master": "",
						"node.openshift.io/os_id": "rhcos",
						"kubernetes.io/arch": "amd64",
						"kubernetes.io/hostname": "master-0-1",
						"kubernetes.io/os": "linux"
					},
					"resourcePoolID": "local-cluster"
				}
			]`)
			request, _ := http.NewRequest("GET", "https://"+os.Getenv("TEST_HOST")+"/o2ims-infrastructureInventory/v1/resourcePools/local-cluster/resources", bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")

			By("Executing https petition")
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			By("Checking response JSON is equal to expected JSON")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJSON(requestBody))
		})
	})
})

var _ = Describe("Alarm Server API testing", func() {
	Context("When getting Resource List from a defined pool", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(``)
			request, _ := http.NewRequest("GET", "http://"+os.Getenv("TEST_HOST")+"/o2ims-infrastructureInventory/v1/resourcePools/local-cluster/resources", bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")

			By("Executing https petition")
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			By("Checking response JSON is equal to expected JSON")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJSON(requestBody))
		})
	})
})

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package models_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

var _ = Describe("RedactDeploymentManagerCredentials", func() {
	It("should remove profileData from extensions", func() {
		extensions := map[string]interface{}{
			"profileName": "k8s",
			"profileData": map[string]interface{}{
				"admin_client_cert":    "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----",
				"admin_client_key":     "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----",
				"admin_user":           "admin",
				"cluster_ca_cert":      "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----",
				"cluster_api_endpoint": "https://api.cluster.example.com:6443",
			},
			"artifactResourceId": "some-template-id",
		}
		dm := generated.DeploymentManager{
			Extensions: &extensions,
		}

		models.RedactDeploymentManagerCredentials(&dm)

		Expect(dm.Extensions).ToNot(BeNil())
		Expect(*dm.Extensions).ToNot(HaveKey("profileData"))
		Expect(*dm.Extensions).To(HaveKeyWithValue("profileName", "k8s"))
		Expect(*dm.Extensions).To(HaveKeyWithValue("artifactResourceId", "some-template-id"))
	})

	It("should be a no-op when extensions is nil", func() {
		dm := generated.DeploymentManager{
			Extensions: nil,
		}

		models.RedactDeploymentManagerCredentials(&dm)

		Expect(dm.Extensions).To(BeNil())
	})

	It("should be a no-op when profileData is not present", func() {
		extensions := map[string]interface{}{
			"profileName": "k8s",
		}
		dm := generated.DeploymentManager{
			Extensions: &extensions,
		}

		models.RedactDeploymentManagerCredentials(&dm)

		Expect(*dm.Extensions).To(HaveKeyWithValue("profileName", "k8s"))
		Expect(*dm.Extensions).ToNot(HaveKey("profileData"))
	})
})

var _ = Describe("DeploymentManagerRedactedConverter", func() {
	It("should redact profileData credentials from the converted model", func() {
		record := models.DeploymentManager{
			DeploymentManagerID: uuid.New(),
			Name:                "test-cluster",
			Description:         "test deployment manager",
			OCloudID:            uuid.New(),
			URL:                 "https://api.test.example.com:6443",
			Locations:           []string{"location-1"},
			Extensions: map[string]interface{}{
				"profileName": "k8s",
				"profileData": map[string]interface{}{
					"admin_client_cert":    "-----BEGIN CERTIFICATE-----\nsensitive\n-----END CERTIFICATE-----",
					"admin_client_key":     "-----BEGIN PRIVATE KEY-----\nsensitive\n-----END PRIVATE KEY-----",
					"admin_user":           "admin",
					"cluster_ca_cert":      "-----BEGIN CERTIFICATE-----\nsensitive\n-----END CERTIFICATE-----",
					"cluster_api_endpoint": "https://api.cluster.example.com:6443",
				},
				"artifactResourceId": "some-template-id",
			},
		}

		result := models.DeploymentManagerRedactedConverter(record)
		model, ok := result.(generated.DeploymentManager)
		Expect(ok).To(BeTrue())

		Expect(model.Extensions).ToNot(BeNil())
		Expect(*model.Extensions).ToNot(HaveKey("profileData"))
		Expect(*model.Extensions).To(HaveKeyWithValue("profileName", "k8s"))
		Expect(*model.Extensions).To(HaveKeyWithValue("artifactResourceId", "some-template-id"))
		Expect(model.DeploymentManagerId).To(Equal(record.DeploymentManagerID))
		Expect(model.Name).To(Equal("test-cluster"))
	})
})

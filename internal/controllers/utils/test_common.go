/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"fmt"

	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

const TestClusterInstanceSpecOk = `
group: siteconfig.open-cluster-management.io
scope: Namespaced
versions:
- name: v1alpha1
  schema:
    openAPIV3Schema:
      description: ClusterInstance is the Schema for the clusterinstances API
      properties:
        apiVersion:
          type: string
        kind:
          type: string
        metadata:
          type: object
        spec:
          properties:
            additionalNTPSources:
              items:
                type: string
              type: array
            apiVIPs:
              items:
                type: string
              maxItems: 2
              type: array
            baseDomain:
              type: string
            clusterImageSetNameRef:
              type: string
            pullSecretRef:
              properties:
                name:
                  type: string
              type: object
              x-kubernetes-map-type: atomic
            nodes:
              items:
                properties:
                  automatedCleaningMode:
                    type: string
                  bootMode:
                    type: string
                  hostName:
                    type: string
                  ironicInspect:
                    type: string
                  templateRefs:
                    items:
                      properties:
                        name:
                          type: string
                        namespace:
                          type: string
                    type: array
                  nodeNetwork:
                    properties:
                      interfaces:
                        items:
                          properties:
                            macAddress:
                              type: string
                            name:
                              type: string
                        type: array
                    type: object
                  role:
                    type: string
              type: array
            templateRefs:
              items:
                properties:
                  name:
                    type: string
                  namespace:
                    type: string
              type: array
          type: object
          required:
          - baseDomain
          - clusterName
      type: object
  served: true
  storage: true
`
const TestClusterInstanceSpecNoVersions = `
group: siteconfig.open-cluster-management.io
scope: Namespaced
openAPIV3Schema:
  description: ClusterInstance is the Schema for the clusterinstances API
  properties:
  apiVersion:
    type: string
  kind:
    type: string
  metadata:
    type: object
  spec:
    properties:
      additionalNTPSources:
        items:
          type: string
        type: array
      baseDomain:
        type: string
    type: object
    required:
    - baseDomain
served: true
storage: true
`
const TestClusterInstanceSpecServedFalse = `
group: siteconfig.open-cluster-management.io
scope: Namespaced
versions:
- name: v1alpha1
  served: false
  storage: true
`

const TestClusterInstancePropertiesRequiredRemoval = `
properties:
  apiVIPs:
    items:
      type: string
    maxItems: 2
    type: array
  baseDomain:
    type: string
  clusterImageSetNameRef:
    type: string
  machineNetwork:
    items:
      properties:
        cidr:
          type: string
      required:
      - cidr
      type: object
    type: array
  nodes:
    items:
      properties:
        bmcAddress:
          type: string
        nodeNetwork:
          properties:
            interfaces:
              items:
                properties:
                  macAddress:
                    type: string
                  name:
                    type: string
                type: object
                required:
                - macAddress
                - name
              type: array
          type: object
    type: array
type: object
required:
- baseDomain
- clusterName
`

func BuildTestClusterInstanceCRD(clusterInstanceSpecStr string) (*unstructured.Unstructured, error) {
	clusterInstanceCrd := &unstructured.Unstructured{}
	clusterInstanceCrd.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	clusterInstanceCrd.SetName(fmt.Sprintf("%s.%s", ClusterInstanceCrdName, siteconfig.Group))

	var clusterInstanceSpecIntf map[string]interface{}
	err := yaml.Unmarshal([]byte(clusterInstanceSpecStr), &clusterInstanceSpecIntf)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling error: %w", err)
	}
	clusterInstanceCrd.Object["spec"] = clusterInstanceSpecIntf

	return clusterInstanceCrd, nil
}

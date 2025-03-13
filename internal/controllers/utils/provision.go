/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"time"

	sprig "github.com/go-task/slim-sprig/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/files"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// ClusterInstanceParamsSubSchemaForNoHWTemplate is the expected subschema for the
// ClusterInstanceParams when no hardware template is provided.
const ClusterInstanceParamsSubSchemaForNoHWTemplate = `
type: object
properties:
  nodes:
    items:
      properties:
        bmcAddress:
          type: string
        bmcCredentialsDetails:
          type: object
          properties:
            username:
              type: string
            password:
              type: string
          required:
          - username
          - password
        bootMACAddress:
          type: string
        nodeNetwork:
          type: object
          properties:
            interfaces:
              type: array
              items:
                type: object
                properties:
                  macAddress:
                    type: string
                required:
                - macAddress
          required:
          - interfaces
      required:
      - bmcAddress
      - bmcCredentialsDetails
      - bootMACAddress
      - nodeNetwork
required:
- nodes
`

// ExtractSchemaRequired extracts the required field of a subschema
func ExtractSchemaRequired(mainSchema []byte) (required []string, err error) {
	requireListAny, err := provisioningv1alpha1.ExtractMatchingInput(mainSchema, requiredString)
	if err != nil {
		return required, fmt.Errorf("could not extract the 'required' section of schema: %w", err)
	}
	requiredAny, ok := requireListAny.([]any)
	if !ok {
		return required, fmt.Errorf("could not cast 'required' section as []any")
	}
	for _, item := range requiredAny {
		itemString, ok := item.(string)
		if !ok {
			return required, fmt.Errorf(`could not cast 'required' section item as a string`)
		}
		required = append(required, itemString)
	}
	return required, nil
}

// ExtractTimeoutFromConfigMap extracts the timeout config from the ConfigMap by key if exits.
// converting it from duration string to time.Duration. Returns an error if the value is not a
// valid duration string.
func ExtractTimeoutFromConfigMap(cm *corev1.ConfigMap, key string) (time.Duration, error) {
	if timeoutStr, err := GetConfigMapField(cm, key); err == nil {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return 0, NewInputError("the value of key %s from ConfigMap %s is not a valid duration string: %v", key, cm.GetName(), err)
		}
		return timeout, nil
	}

	return 0, nil
}

// RenderTemplateForK8sCR returns a rendered K8s resource with an given template and object data
func RenderTemplateForK8sCR(templateName, templatePath string, templateDataObj map[string]any) (*unstructured.Unstructured, error) {
	renderedTemplate := &unstructured.Unstructured{}

	// Load the template from yaml file
	tmplContent, err := files.Controllers.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %s, err: %w", templatePath, err)
	}

	// Create a FuncMap with template functions
	funcMap := sprig.TxtFuncMap()
	funcMap["toYaml"] = toYaml
	funcMap["validateNonEmpty"] = validateNonEmpty
	funcMap["validateArrayType"] = validateArrayType
	funcMap["validateMapType"] = validateMapType

	// Parse the template
	tmpl, err := template.New(templateName).Funcs(funcMap).Parse(string(tmplContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template content from template file: %s, err: %w", templatePath, err)
	}

	// Execute the template with the data
	var output bytes.Buffer
	err = tmpl.Execute(&output, templateDataObj)
	if err != nil {
		return nil, fmt.Errorf("failed to execute template %s with data, err: %w", templateName, err)
	}

	err = yaml.Unmarshal(output.Bytes(), renderedTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal, err: %w", err)
	}

	return renderedTemplate, nil
}

// toYaml converts an interface to a YAML string and trims the trailing newline
func toYaml(v any) (string, error) {
	// yaml.Marshal adds a trailing newline to its output
	yamlData, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("failed to marshal interface to YAML: %w", err)
	}

	return strings.TrimRight(string(yamlData), "\n"), nil
}

// validateNonEmpty validates the input and fails if it is not provided or empty
func validateNonEmpty(fieldName string, input any) (any, error) {
	// Check if the input is empty (you can adjust this condition as per your validation logic)
	if input == nil {
		return nil, fmt.Errorf("%s must be provided", fieldName)
	}

	v := reflect.ValueOf(input)
	if v.Kind() == reflect.String || v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
		if v.Len() == 0 {
			return nil, fmt.Errorf("%s cannot be empty", fieldName)
		}
	}

	return input, nil
}

// validateMapType checks if the input is of type map and raises an error if not.
func validateMapType(fieldName string, input any) (any, error) {
	if reflect.TypeOf(input).Kind() != reflect.Map {
		return nil, fmt.Errorf("%s must be of type map", fieldName)
	}
	return input, nil
}

// validateArrayType checks if the input is of type slice (array) and raises an error if not.
func validateArrayType(fieldName string, input any) (any, error) {
	if reflect.TypeOf(input).Kind() != reflect.Slice {
		return nil, fmt.Errorf("%s must be of type array", fieldName)
	}
	return input, nil
}

// TimeoutExceeded returns true if it's been more time than the timeout configuration.
func TimeoutExceeded(startTime time.Time, timeout time.Duration) bool {
	return time.Since(startTime) > timeout
}

// ClusterIsReadyForPolicyConfig checks if a cluster is ready for policy configuration
// by looking at its availability, joined status and hub acceptance.
func ClusterIsReadyForPolicyConfig(
	ctx context.Context, c client.Client, clusterInstanceName string) (bool, error) {
	// Check if the managed cluster is available. If not, return.
	managedCluster := &clusterv1.ManagedCluster{}
	managedClusterExists, err := DoesK8SResourceExist(
		ctx, c, clusterInstanceName, "", managedCluster)
	if err != nil {
		return false, fmt.Errorf("failed to check if managed cluster exists: %w", err)
	}
	if !managedClusterExists {
		return false, nil
	}

	available := false
	hubAccepted := false
	joined := false

	availableCondition := meta.FindStatusCondition(
		managedCluster.Status.Conditions,
		clusterv1.ManagedClusterConditionAvailable)
	if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
		available = true
	}

	acceptedCondition := meta.FindStatusCondition(
		managedCluster.Status.Conditions,
		clusterv1.ManagedClusterConditionHubAccepted)
	if acceptedCondition != nil && acceptedCondition.Status == metav1.ConditionTrue {
		hubAccepted = true
	}

	joinedCondition := meta.FindStatusCondition(
		managedCluster.Status.Conditions,
		clusterv1.ManagedClusterConditionJoined)
	if joinedCondition != nil && joinedCondition.Status == metav1.ConditionTrue {
		joined = true
	}

	return available && hubAccepted && joined, nil
}

// ValidateDefaultInterfaces verifies that each interface has a specified label field,
// as labels are not part of the ClusterInstance structure by default.
func ValidateDefaultInterfaces[T any](data T) error {
	// clusterinstance-default data
	dataMap, _ := any(data).(map[string]any)
	nodes, ok := dataMap["nodes"].([]any)
	if ok {
		for _, node := range nodes {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected: invalid node data structure")
			}
			interfaces := getInterfaces(nodeMap)
			if interfaces == nil {
				return fmt.Errorf("failed to extract the interfaces from the node map")
			}
			for _, intf := range interfaces {
				value, exists := intf["label"]
				if !exists {
					return fmt.Errorf("'label' is missing for interface: %s", intf["name"])
				}
				if value == "" {
					return fmt.Errorf("'label' is empty for interface: %s", intf["name"])
				}
			}
		}
	}
	return nil
}

// removeLabelFromInterfaces removed the label property for each interface as the label
// property is not part of the ClusterInstance schema.
func removeLabelFromInterfaces[T any](data T) error {
	dataMap, _ := any(data).(map[string]any)
	nodes, ok := dataMap["nodes"].([]any)
	if ok {
		for _, node := range nodes {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected: invalid node data structure")
			}
			interfaces := getInterfaces(nodeMap)
			if interfaces == nil {
				return fmt.Errorf("failed to extract the interfaces from the node map")
			}
			for _, intf := range interfaces {
				delete(intf, "label")
			}
		}
	}
	return nil
}

// removeRequiredFromSchema removes all the requiered properties from a map.
func removeRequiredFromSchema(schema map[string]any) {
	// Check if the current schema level has "properties" defined.
	if properties, hasProperties := schema["properties"]; hasProperties {
		delete(schema, "required")

		// Recurse into each property defined under "properties"
		if propsMap, ok := properties.(map[string]any); ok {
			for _, propValue := range propsMap {
				if propSchema, ok := propValue.(map[string]any); ok {
					removeRequiredFromSchema(propSchema)
				}
			}
		}
	}

	// Recurse into each property defined under "items"
	if items, hasItems := schema["items"]; hasItems {
		if itemSchema, ok := items.(map[string]any); ok {
			removeRequiredFromSchema(itemSchema)
		}
	}
}

// ValidateConfigmapSchemaAgainstClusterInstanceCRD checks if the data of the ClusterInstance
// default ConfigMap matches the ClusterInstance CRD schema.
func ValidateConfigmapSchemaAgainstClusterInstanceCRD[T any](ctx context.Context, c client.Client, data T) error {
	// Get the ClusterInstance CRD.
	clusterInstanceCrd := &unstructured.Unstructured{}
	clusterInstanceCrd.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	crdName := fmt.Sprintf("%s.%s", ClusterInstanceCrdName, siteconfig.Group)
	err := c.Get(ctx, types.NamespacedName{Name: crdName}, clusterInstanceCrd)
	if err != nil {
		return fmt.Errorf("failed to obtain the %s.%s CRD: %w", ClusterInstanceCrdName, siteconfig.Group, err)
	}

	// Extract the OpenAPIV3Schema.
	openAPIV3Schema := make(map[string]interface{})
	versions, found, err := unstructured.NestedSlice(clusterInstanceCrd.Object, "spec", "versions")
	if err != nil || !found {
		return fmt.Errorf("failed to obtain the versions of the %s.%s CRD: %w", ClusterInstanceCrdName, siteconfig.Group, err)
	}

	// Find the version that is stored and served.
	for index, version := range versions {
		versionMap, ok := version.(map[string]interface{})
		if !ok {
			return fmt.Errorf(
				"failed to convert version %d of the %s.%s CRD to map: %w",
				index, ClusterInstanceCrdName, siteconfig.Group, err)
		}
		if versionMap["served"] != true || versionMap["storage"] != true {
			continue
		}
		// Extract the schema.
		openAPIV3Schema, found, err = unstructured.NestedMap(versionMap, "schema", "openAPIV3Schema")
		if err != nil || !found {
			return fmt.Errorf(
				"failed to obtain the openAPIV3Schema from version %d of the %s.%s CRD: %w",
				index, ClusterInstanceCrdName, siteconfig.Group, err)
		}
		break
	}
	if len(openAPIV3Schema) == 0 {
		return fmt.Errorf("no version served & stored in the %s.%s CRD ", ClusterInstanceCrdName, siteconfig.Group)
	}

	// If the properties and spec attributes are missing or the conversion fails, then something is wrong
	// with k8s itself.
	openAPIV3SchemaSpec := openAPIV3Schema["properties"].(map[string]interface{})["spec"].(map[string]interface{})

	// Prepare the data for schema validation.
	// Remove the `required` property as the default ConfigMaps contains only a subset of the ClusterInstance spec.
	removeRequiredFromSchema(openAPIV3SchemaSpec)
	// Disalllow unknown properties in the ClusterInstance CRD schema.
	provisioningv1alpha1.DisallowUnknownFieldsInSchema(openAPIV3SchemaSpec)
	// Remove the interface label properties as it's not part of the ClusterInstance CRD schema.
	dataMap, _ := any(data).(map[string]any)
	if err := removeLabelFromInterfaces(dataMap); err != nil {
		return fmt.Errorf("error removing label from interfaces")
	}

	err = provisioningv1alpha1.ValidateJsonAgainstJsonSchema(openAPIV3SchemaSpec, dataMap)
	if err != nil {
		return fmt.Errorf("the ConfigMap does not match the ClusterInstance schema: %w", err)
	}
	return nil
}

// GetParentPolicyNameAndNamespace extracts the parent policy name and namespace
// from the child policy name. The child policy name follows the format:
// "<parent_policy_namespace>.<parent_policy_name>". Since the namespace is disallowed
// to contain ".", splitting the string with "." into two substrings is safe.
func GetParentPolicyNameAndNamespace(childPolicyName string) (policyName, policyNamespace string) {
	res := strings.SplitN(childPolicyName, ".", 2)
	return res[1], res[0]
}

// IsParentPolicyInZtpClusterTemplateNs checks whether the parent policy resides
// in the namespace "ztp-<clustertemplate-ns>".
func IsParentPolicyInZtpClusterTemplateNs(policyNamespace, ctNamespace string) bool {
	return policyNamespace == fmt.Sprintf("ztp-%s", ctNamespace)
}

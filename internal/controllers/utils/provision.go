package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"time"

	sprig "github.com/go-task/slim-sprig/v3"
	diff "github.com/r3labs/diff/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/openshift-kni/oran-o2ims/internal/files"
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

// ExtractSubSchema extracts a Sub schema indexed by subSchemaKey from a Main schema
func ExtractSubSchema(mainSchema []byte, subSchemaKey string) (subSchema map[string]any, err error) {
	jsonObject := make(map[string]any)
	if len(mainSchema) == 0 {
		return subSchema, nil
	}
	err = json.Unmarshal(mainSchema, &jsonObject)
	if err != nil {
		return subSchema, fmt.Errorf("failed to UnMarshall Main Schema: %w", err)
	}
	if _, ok := jsonObject[PropertiesString]; !ok {
		return subSchema, fmt.Errorf("non compliant Main Schema, missing 'properties' section: %w", err)
	}
	properties, ok := jsonObject[PropertiesString].(map[string]any)
	if !ok {
		return subSchema, fmt.Errorf("could not cast 'properties' section of schema as map[string]any: %w", err)
	}

	subSchemaValue, ok := properties[subSchemaKey]
	if !ok {
		return subSchema, fmt.Errorf("subSchema '%s' does not exist: %w", subSchemaKey, err)
	}

	subSchema, ok = subSchemaValue.(map[string]any)
	if !ok {
		return subSchema, fmt.Errorf("subSchema '%s' is not a valid map: %w", subSchemaKey, err)
	}
	return subSchema, nil
}

// ExtractMatchingInput extracts the portion of the input data that corresponds to a given subSchema key.
func ExtractMatchingInput(parentSchema []byte, subSchemaKey string) (any, error) {
	inputData := make(map[string]any)
	err := json.Unmarshal(parentSchema, &inputData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal parent schema: %w", err)
	}

	// Check if the input contains the subSchema key
	matchingInput, ok := inputData[subSchemaKey]
	if !ok {
		return nil, fmt.Errorf("parent schema does not contain key '%s': %w", subSchemaKey, err)
	}
	return matchingInput, nil
}

// ExtractSchemaRequired extracts the required field of a subschema
func ExtractSchemaRequired(mainSchema []byte) (required []string, err error) {
	requireListAny, err := ExtractMatchingInput(mainSchema, requiredString)
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

// FindClusterInstanceImmutableFieldUpdates identifies updates made to immutable fields
// in the ClusterInstance spec. It returns two lists of paths: a list of updated fields
// that are considered immutable and should not be modified and a list of fields related
// to node scaling, indicating nodes that were added or removed.
func FindClusterInstanceImmutableFieldUpdates(
	oldClusterInstance, newClusterInstance *unstructured.Unstructured) ([]string, []string, error) {

	oldClusterInstanceSpec := oldClusterInstance.Object["spec"].(map[string]any)
	newClusterInstanceSpec := newClusterInstance.Object["spec"].(map[string]any)

	diffs, err := diff.Diff(oldClusterInstanceSpec, newClusterInstanceSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("error comparing differences between existing "+
			"and newly rendered ClusterInstance: %w", err)
	}

	var updatedFields []string
	var scalingNodes []string
	for _, diff := range diffs {
		/* Examples of diff result in json format

		Label added at the cluster-level
		  {"type": "create", "path": ["extraLabels", "ManagedCluster", "newLabelKey"], "from": null, "to": "newLabelValue"}

		Field updated at the cluster-level
		  {"type": "update", "path": ["baseDomain"], "from": "domain.example.com", "to": "newdomain.example.com"}

		New node added
		  {"type": "create", "path": ["nodes", "1"], "from": null, "to": {"hostName": "worker2"}}

		Existing node removed
		  {"type": "delete", "path": ["nodes", "1"], "from": {"hostName": "worker2"}, "to": null}

		Field updated at the node-level
		  {"type": "update", "path": ["nodes", "0", "nodeNetwork", "config", "dns-resolver", "config", "server", "0"], "from": "192.10.1.2", "to": "192.10.1.3"}
		*/

		// Check if the path matches any ignored fields
		if matchesAnyPattern(diff.Path, IgnoredClusterInstanceFields) {
			// Ignored field; skip
			continue
		}

		oranUtilsLog.Info(
			fmt.Sprintf(
				"Detected field change in the newly rendered ClusterInstance(%s) type: %s, path: %s, from: %+v, to: %+v",
				oldClusterInstance.GetName(), diff.Type, strings.Join(diff.Path, "."), diff.From, diff.To,
			),
		)

		// Check if the path matches any allowed fields
		if matchesAnyPattern(diff.Path, AllowedClusterInstanceFields) {
			// Allowed field; skip
			continue
		}

		// Check if the change is adding or removing a node.
		// Path like ["nodes", "1"], indicating node addition or removal.
		if diff.Path[0] == "nodes" && len(diff.Path) == 2 {
			scalingNodes = append(scalingNodes, strings.Join(diff.Path, "."))
			continue
		}
		updatedFields = append(updatedFields, strings.Join(diff.Path, "."))
	}

	return updatedFields, scalingNodes, nil
}

// matchesPattern checks if the path matches the pattern
func matchesPattern(path, pattern []string) bool {
	if len(path) < len(pattern) {
		return false
	}

	for i, p := range pattern {
		if p == "*" {
			// Wildcard matches any single element
			continue
		}
		if path[i] != p {
			return false
		}
	}

	return true
}

// matchesAnyPattern checks if the given path matches any pattern in the provided list.
func matchesAnyPattern(path []string, patterns [][]string) bool {
	for _, pattern := range patterns {
		if matchesPattern(path, pattern) {
			return true
		}
	}
	return false
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

package utils

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"k8s.io/apimachinery/pkg/util/net"
	ctrl "sigs.k8s.io/controller-runtime"

	sprig "github.com/go-task/slim-sprig/v3"
	diff "github.com/r3labs/diff/v3"
	"github.com/xeipuuv/gojsonschema"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/files"
	openshiftv1 "github.com/openshift/api/config/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

const (
	PropertiesString = "properties"
)

var (
	oranUtilsLog         = ctrl.Log.WithName("oranUtilsLog")
	hwMgrPluginNameSpace string
	once                 sync.Once
)

func UpdateK8sCRStatus(ctx context.Context, c client.Client, object client.Object) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Status().Update(ctx, object); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("status update failed after retries: %w", err)
	}

	return nil
}

// DisallowUnknownFieldsInSchema updates a schema by adding "additionalProperties": false
// to all objects/arrays that define "properties". This ensures that any unknown fields
// not defined in the schema will be disallowed during validation.
func DisallowUnknownFieldsInSchema(schema map[string]any) {
	// Check if the current schema level has "properties" defined
	if properties, hasProperties := schema["properties"]; hasProperties {
		// If "additionalProperties" is not already set, add it with the value false
		if _, exists := schema["additionalProperties"]; !exists {
			schema["additionalProperties"] = false
		}

		// Recurse into each property defined under "properties"
		if propsMap, ok := properties.(map[string]any); ok {
			for _, propValue := range propsMap {
				if propSchema, ok := propValue.(map[string]any); ok {
					DisallowUnknownFieldsInSchema(propSchema)
				}
			}
		}
	}

	// Recurse into each property defined under "items"
	if items, hasItems := schema["items"]; hasItems {
		if itemSchema, ok := items.(map[string]any); ok {
			DisallowUnknownFieldsInSchema(itemSchema)
		}
	}

	// Ignore other keywords that could have "properties"
}

func ValidateJsonAgainstJsonSchema(schema, input any) error {
	schemaLoader := gojsonschema.NewGoLoader(schema)
	inputLoader := gojsonschema.NewGoLoader(input)

	result, err := gojsonschema.Validate(schemaLoader, inputLoader)
	if err != nil {
		oranUtilsLog.Error(err, "Error validating the input against the schema")
		return fmt.Errorf("failed when validating the input against the schema: %w", err)
	}

	if result.Valid() {
		return nil
	} else {
		var errs []string
		for _, description := range result.Errors() {
			errs = append(errs, description.String())
		}

		return fmt.Errorf("invalid input: %s", strings.Join(errs, "; "))
	}
}

func GetBMCDetailsForClusterInstance(node map[string]interface{}, provisioningRequest string) (
	string, string, string, error) {
	// Get the BMC details.
	bmcCredentialsDetailsInterface, bmcCredentialsDetailsExist := node["bmcCredentialsDetails"]

	if !bmcCredentialsDetailsExist {
		return "", "", "", NewInputError(
			`\"bmcCredentialsDetails\" key expected to exist in ClusterTemplateInput 
			of ProvisioningRequest %s, but it's missing`,
			provisioningRequest,
		)
	}

	bmcCredentialsDetails := bmcCredentialsDetailsInterface.(map[string]interface{})

	// Get the BMC username and password.
	username, usernameExists := bmcCredentialsDetails["username"].(string)
	if !usernameExists {
		return "", "", "", NewInputError(
			`\"bmcCredentialsDetails.username\" key expected to exist in ClusterTemplateInput 
			of ProvisioningRequest %s, but it's missing`,
			provisioningRequest,
		)
	}

	password, passwordExists := bmcCredentialsDetails["password"].(string)
	if !passwordExists {
		return "", "", "", NewInputError(
			`\"bmcCredentialsDetails.password\" key expected to exist in ClusterTemplateInput 
			of ProvisioningRequest %s, but it's missing`,
			provisioningRequest,
		)
	}

	secretName := ""
	// Get the BMC CredentialsName.
	bmcCredentialsNameInterface, bmcCredentialsNameExist := node["bmcCredentialsName"]
	if !bmcCredentialsNameExist {
		nodeHostnameInterface, nodeHostnameExists := node["hostName"]
		if !nodeHostnameExists {
			secretName = provisioningRequest
		} else {
			secretName =
				extractBeforeDot(strings.ToLower(nodeHostnameInterface.(string))) +
					"-bmc-secret"
		}
	} else {
		secretName = bmcCredentialsNameInterface.(map[string]interface{})["name"].(string)
	}

	return username, password, secretName, nil
}

// CreateK8sCR creates/updates/patches an object.
func CreateK8sCR(ctx context.Context, c client.Client,
	newObject client.Object, ownerObject client.Object,
	operation string) (err error) {

	// Get the name and namespace of the object:
	key := client.ObjectKeyFromObject(newObject)
	oranUtilsLog.Info("[CreateK8sCR] Resource", "name", key.Name, "namespace", key.Namespace, "kind", newObject.GetObjectKind().GroupVersionKind().Kind)

	// We can set the owner reference only for objects that live in the same namespace, as cross
	// namespace owners are forbidden. This also applies to non-namespaced objects like cluster
	// roles or cluster role bindings; those have empty namespaces, so the equals comparison
	// should also work.
	if ownerObject != nil && ownerObject.GetNamespace() == key.Namespace {
		err = controllerutil.SetControllerReference(ownerObject, newObject, c.Scheme())
		if err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}
	}

	// Create an empty object of the same type of the new object. We will use it to fetch the
	// current state.
	objectType := reflect.TypeOf(newObject).Elem()
	oldObject := reflect.New(objectType).Interface().(client.Object)

	// If the newObject is unstructured, we need to copy the GVK to the oldObject.
	if unstructuredObj, ok := newObject.(*unstructured.Unstructured); ok {
		oldUnstructuredObj := oldObject.(*unstructured.Unstructured)
		oldUnstructuredObj.SetGroupVersionKind(unstructuredObj.GroupVersionKind())
	}

	err = c.Get(ctx, key, oldObject)

	// If there was an error obtaining the CR and the error was "Not found", create the object.
	// If any other occurred, return the error.
	// If the CR already exists, patch it or update it.
	if err != nil {
		if errors.IsNotFound(err) {
			oranUtilsLog.Info(
				"[CreateK8sCR] CR not found, CREATE it",
				"name", newObject.GetName(),
				"namespace", newObject.GetNamespace())
			err = c.Create(ctx, newObject)
			if err != nil {
				return fmt.Errorf("failed to create CR %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
			}
		} else {
			return fmt.Errorf("failed to get CR %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
		}
	} else {
		newObject.SetResourceVersion(oldObject.GetResourceVersion())
		if operation == PATCH {
			oranUtilsLog.Info("[CreateK8sCR] CR already present, PATCH it",
				"name", newObject.GetName(),
				"namespace", newObject.GetNamespace())
			if err := c.Patch(ctx, newObject, client.MergeFrom(oldObject)); err != nil {
				return fmt.Errorf("failed to patch object %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
			}
			return nil
		} else if operation == UPDATE {
			oranUtilsLog.Info("[CreateK8sCR] CR already present, UPDATE it",
				"name", newObject.GetName(),
				"namespace", newObject.GetNamespace())
			if err := c.Update(ctx, newObject); err != nil {
				return fmt.Errorf("failed to update object %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
			}
			return nil
		}
	}

	return nil
}

func DoesK8SResourceExist(ctx context.Context, c client.Client, name, namespace string, obj client.Object) (resourceExists bool, err error) {
	err = c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)

	if err != nil {
		if errors.IsNotFound(err) {
			oranUtilsLog.Info("[doesK8SResourceExist] Resource not found, create it. ",
				"name", name, "namespace", namespace)
			return false, nil
		} else {
			return false, fmt.Errorf("failed to check existence of resource '%s' in namespace '%s': %w", name, namespace, err)
		}
	} else {
		oranUtilsLog.Info("[doesK8SResourceExist] Resource already present, return. ",
			"name", name, "namespace", namespace)
		return true, nil
	}
}

func extensionsToExtensionArgs(extensions []string) []string {
	var extensionsArgsArray []string
	for _, crtExt := range extensions {
		newExtensionFlag := "--extensions=" + crtExt
		extensionsArgsArray = append(extensionsArgsArray, newExtensionFlag)
	}

	return extensionsArgsArray
}

// HasApiEndpoints determines whether a server exposes a set of API endpoints
func HasApiEndpoints(serverName string) bool {
	return serverName == InventoryMetadataServerName ||
		serverName == InventoryResourceServerName ||
		serverName == InventoryDeploymentManagerServerName ||
		serverName == InventoryAlarmSubscriptionServerName
}

func GetDeploymentVolumes(serverName string) []corev1.Volume {
	if HasApiEndpoints(serverName) {
		return []corev1.Volume{
			{
				Name: "tls",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: fmt.Sprintf("%s-tls", serverName),
					},
				},
			},
		}
	}

	return []corev1.Volume{}
}

func GetDeploymentVolumeMounts(serverName string) []corev1.VolumeMount {
	if HasApiEndpoints(serverName) {
		return []corev1.VolumeMount{
			{
				Name:      "tls",
				MountPath: "/secrets/tls",
			},
		}
	}

	return []corev1.VolumeMount{}
}

func GetBackendTokenArg(backendToken string) string {
	// If no backend token has been provided then use the token of the service account
	// that will eventually execute the server. Note that the file may not exist,
	// but we can't check it here as that will be a different pod.
	if backendToken != "" {
		return fmt.Sprintf("--backend-token=%s", backendToken)
	}

	return fmt.Sprintf("--backend-token-file=%s", defaultBackendTokenFile)
}

// getACMNamespace will determine the ACM namespace from the multiclusterengine object.
//
// multiclusterengine object sample:
//
//	apiVersion: multicluster.openshift.io/v1
//	kind: MultiClusterEngine
//	metadata:
//	  labels:
//	    installer.name: multiclusterhub
//	    installer.namespace: open-cluster-management
func getACMNamespace(ctx context.Context, c client.Client) (string, error) {
	// Get the multiclusterengine object.
	multiClusterEngine := &unstructured.Unstructured{}
	multiClusterEngine.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "multicluster.openshift.io",
		Kind:    "MultiClusterEngine",
		Version: "v1",
	})
	err := c.Get(ctx, client.ObjectKey{
		Name: "multiclusterengine",
	}, multiClusterEngine)

	if err != nil {
		oranUtilsLog.Info("[getACMNamespace] multiclusterengine object not found")
		return "", fmt.Errorf("multiclusterengine object not found")
	}

	// Get the ACM namespace by looking at the installer.namespace label.
	multiClusterEngineMetadata := multiClusterEngine.Object["metadata"].(map[string]interface{})
	multiClusterEngineLabels, labelsOk := multiClusterEngineMetadata["labels"]

	if labelsOk {
		acmNamespace, acmNamespaceOk := multiClusterEngineLabels.(map[string]interface{})["installer.namespace"]

		if !acmNamespaceOk {
			return "", fmt.Errorf("multiclusterengine labels do not contain the installer.namespace key")
		}
		return acmNamespace.(string), nil
	}

	return "", fmt.Errorf("multiclusterengine object does not have expected labels")
}

// getSearchAPI will dynamically obtain the search API.
func getSearchAPI(ctx context.Context, c client.Client, inventory *inventoryv1alpha1.Inventory) (string, error) {
	// Find the ACM namespace.
	acmNamespace, err := getACMNamespace(ctx, c)
	if err != nil {
		return "", err
	}

	// Split the Ingress to obtain the domain for the Search API.
	// searchAPIBackendURL example: https://search-api-open-cluster-management.apps.lab.karmalabs.corp
	// IngressHost example:         o2ims.apps.lab.karmalabs.corp
	// Note: The domain could also be obtained from the spec.host of the search-api route in the
	// ACM namespace.
	ingressSplit := strings.Split(inventory.Spec.IngressHost, ".apps")
	if len(ingressSplit) != 2 {
		return "", fmt.Errorf("the searchAPIBackendURL could not be obtained from the IngressHost. " +
			"Directly specify the searchAPIBackendURL in the Inventory CR or update the IngressHost")
	}
	domain := ".apps" + ingressSplit[len(ingressSplit)-1]

	// The searchAPI is obtained from the "search-api" string and the ACM namespace.
	searchAPI := "https://" + "search-api-" + acmNamespace + domain

	return searchAPI, nil
}

func GetServerArgs(ctx context.Context, c client.Client,
	inventory *inventoryv1alpha1.Inventory,
	serverName string) (result []string, err error) {
	// MetadataServer
	if serverName == InventoryMetadataServerName {
		result = slices.Clone(MetadataServerArgs)
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Spec.CloudId),
			fmt.Sprintf("--external-address=https://%s", inventory.Spec.IngressHost))

		return
	}

	// ResourceServer
	if serverName == InventoryResourceServerName {
		searchAPI := inventory.Spec.ResourceServerConfig.BackendURL
		if searchAPI == "" {
			searchAPI, err = getSearchAPI(ctx, c, inventory)
			if err != nil {
				return nil, err
			}
		}

		result = slices.Clone(ResourceServerArgs)

		// Add the cloud-id, backend-url, and token args:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", searchAPI),
			GetBackendTokenArg(inventory.Spec.ResourceServerConfig.BackendToken))

		return result, nil
	}

	// DeploymentManagerServer
	if serverName == InventoryDeploymentManagerServerName {
		result = slices.Clone(DeploymentManagerServerArgs)

		// Set the cloud identifier:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Spec.CloudId),
		)

		// Set the backend type:
		if inventory.Spec.DeploymentManagerServerConfig.BackendType != "" {
			result = append(
				result,
				fmt.Sprintf("--backend-type=%s", inventory.Spec.DeploymentManagerServerConfig.BackendType),
			)
		}

		// If no backend URL has been provided then use the default URL of the Kubernetes
		// API server of the cluster:
		backendURL := inventory.Spec.DeploymentManagerServerConfig.BackendURL
		if backendURL == "" {
			backendURL = defaultBackendURL
		}

		// Add the backend and token args:
		result = append(
			result,
			fmt.Sprintf("--backend-url=%s", backendURL),
			GetBackendTokenArg(inventory.Spec.DeploymentManagerServerConfig.BackendToken))

		// Add the extensions:
		extensionsArgsArray := extensionsToExtensionArgs(inventory.Spec.DeploymentManagerServerConfig.Extensions)
		result = append(result, extensionsArgsArray...)

		return
	}

	return
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

// extractBeforeDot returns the strubstring before the first dot.
func extractBeforeDot(s string) string {
	dotIndex := strings.Index(s, ".")
	if dotIndex == -1 {
		return s
	}
	return s[:dotIndex]
}

// GetSecret attempts to retrieve a Secret object for the given name
func GetSecret(ctx context.Context, c client.Client, name, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	exists, err := DoesK8SResourceExist(ctx, c, name, namespace, secret)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, NewInputError(
			"the Secret '%s' is not found in the namespace '%s'", name, namespace)
	}
	return secret, nil
}

// GetSecretField attempts to retrieve the value of the field using the provided field name
func GetSecretField(secret *corev1.Secret, fieldName string) (string, error) {
	encoded, ok := secret.Data[fieldName]
	if !ok {
		return "", NewInputError("the Secret '%s' does not contain a field named '%s'", secret.Name, fieldName)
	}

	return string(encoded), nil
}

// GetConfigmap attempts to retrieve a ConfigMap object for the given name
func GetConfigmap(ctx context.Context, c client.Client, name, namespace string) (*corev1.ConfigMap, error) {
	existingConfigmap := &corev1.ConfigMap{}
	cmExists, err := DoesK8SResourceExist(
		ctx, c, name, namespace, existingConfigmap)
	if err != nil {
		return nil, err
	}

	if !cmExists {
		// Check if the configmap is missing
		return nil, NewInputError(
			"the ConfigMap '%s' is not found in the namespace '%s'", name, namespace)
	}
	return existingConfigmap, nil
}

// GetConfigMapField attempts to retrieve the value of the field using the provided field name
func GetConfigMapField(cm *corev1.ConfigMap, fieldName string) (string, error) {
	data, ok := cm.Data[fieldName]
	if !ok {
		return data, NewInputError("the ConfigMap '%s' does not contain a field named '%s'", cm.Name, fieldName)
	}

	return data, nil
}

// ExtractTemplateDataFromConfigMap extracts the template data associated with the specified key
// from the provided ConfigMap. The data is expected to be in YAML format.
func ExtractTemplateDataFromConfigMap[T any](ctx context.Context, c client.Client, cm *corev1.ConfigMap, expectedKey string) (T, error) {
	var validData T

	// Find the expected key is present in the configmap data
	defaults, err := GetConfigMapField(cm, expectedKey)
	if err != nil {
		return validData, err
	}

	// Parse the YAML data into a map
	err = yaml.Unmarshal([]byte(defaults), &validData)
	if err != nil {
		return validData, NewInputError(
			"the value of key %s from ConfigMap %s is not in a valid YAML string: %s",
			expectedKey, cm.GetName(), err.Error(),
		)
	}
	return validData, nil
}

// DeepMergeMaps performs a deep merge of the src map into the dst map.
// Merge rules:
//  1. If a key exists in both src and dst maps:
//     a. If the values are of different types and matched type is required,
//     it returns an error, otherwise, the src values overrides the dst element.
//     b. If the values are both maps, recursively merge them.
//     c. If the values are both slices, deeply merge the slices.
//     d. For other types, the src value overrides the dst value.
//  2. If a key exists only in src, add it to dst.
//  3. If a key exists only in dst, preserve it.
func DeepMergeMaps[K comparable, V any](dst, src map[K]V, checkType bool) error {
	for key, srcValue := range src {
		if dstValue, exists := dst[key]; exists {
			if reflect.TypeOf(dstValue) != reflect.TypeOf(srcValue) {
				// If types do not match, return an error if checkType is true
				if checkType {
					return fmt.Errorf("type mismatch for key: %v (dst: %T, src: %T)", key, dstValue, srcValue)
				}
				// Otherwise, override dst with sr
				dst[key] = srcValue
			} else {
				// Types match, handle according to type
				switch dstValueTyped := any(dstValue).(type) {
				case map[K]V:
					// If both values are maps, recursively merge them
					srcValueTyped := any(srcValue).(map[K]V)
					if err := DeepMergeMaps(dstValueTyped, srcValueTyped, checkType); err != nil {
						return fmt.Errorf("error merging maps for key: %v: %w", key, err)
					}
				case []V:
					// If both values are slices, deeply merge the slices
					srcValueTyped := any(srcValue).([]V)
					mergedSlice, err := DeepMergeSlices[K](dstValueTyped, srcValueTyped, checkType)
					if err != nil {
						return fmt.Errorf("error merging slices for key: %v: %w", key, err)
					}
					// Convert the merged slice back to the generic type V
					dst[key] = any(mergedSlice).(V)
				default:
					// For other types, override dst with src
					dst[key] = srcValue
				}
			}
		} else {
			// If the key exists only in src, add it to dst
			dst[key] = srcValue
		}
	}
	return nil
}

// DeepMergeSlices performs a deep indexing merge of the src slice into the dst slice.
// Merge rules:
//  1. For elements present in both src and dst slices at the same index:
//     a. If the elements are of different types and matched type is required,
//     it returns an error, otherwise, the src element overrides the dst element.
//     b. If the elements are both maps, deeply merge them.
//     c. For other types, the src element overrides the dst element.
//  2. If the src slice is longer, append the additional elements from src to dst.
//  3. If the dst slice is longer, preserve the additional elements from dst.
func DeepMergeSlices[K comparable, V any](dst, src []V, checkType bool) ([]V, error) {
	maxLen := len(dst)
	if len(src) > maxLen {
		maxLen = len(src)
	}

	result := make([]V, 0, maxLen)

	for i := 0; i < maxLen; i++ {
		if i < len(dst) && i < len(src) { // nolint: gocritic
			dstElem := dst[i]
			srcElem := src[i]
			if reflect.TypeOf(dstElem) != reflect.TypeOf(srcElem) {
				// If types do not match, return an error if checkType is true
				if checkType {
					return nil, fmt.Errorf("type mismatch at index: %d (dst: %T, src: %T)", i, dstElem, srcElem)
				}
				// Otherwise, use the src element
				result = append(result, srcElem)
			} else {
				// Types match, handle according to type
				switch dstElemTyped := any(dstElem).(type) {
				case map[K]V:
					// If both elements are maps, deeply merge them
					srcElemTyped := any(srcElem).(map[K]V)
					mergedElem := make(map[K]V)
					for k, v := range dstElemTyped {
						mergedElem[k] = v
					}
					if err := DeepMergeMaps(mergedElem, srcElemTyped, checkType); err != nil {
						return nil, fmt.Errorf("error merging maps at slice index: %d: %w", i, err)
					}
					result = append(result, any(mergedElem).(V))
				default:
					// For other types, use the src element
					result = append(result, srcElem)
				}
			}
		} else if i < len(dst) {
			// Only dst has the element
			result = append(result, dst[i])
		} else {
			// Only src has the element
			result = append(result, src[i])
		}
	}

	return result, nil
}

// OverrideClusterInstanceLabelsOrAnnotations handles the overrides of ClusterInstance's extraLabels
// or extraAnnotations. It overrides the values in the ProvisioningRequest with those from the default
// configmap when the same labels/annotations exist in both. Labels/annotations that are not common
// between the default configmap and ProvisioningRequest are ignored.
func OverrideClusterInstanceLabelsOrAnnotations(dstProvisioningRequestInput, srcConfigmap map[string]any) error {
	fields := []string{"extraLabels", "extraAnnotations"}

	for _, field := range fields {
		srcValue, existsSrc := srcConfigmap[field]
		dstValue, existsDst := dstProvisioningRequestInput[field]
		// Check only when both configmap and ProvisioningRequestInput contain the field
		if existsSrc && existsDst {
			dstMap, okDst := dstValue.(map[string]any)
			srcMap, okSrc := srcValue.(map[string]any)
			if !okDst || !okSrc {
				return fmt.Errorf("type mismatch for field %s: (from ProvisioningRequest: %T, from default Configmap: %T)",
					field, dstValue, srcValue)
			}

			// Iterate over the resource types (e.g., ManagedCluster, AgentClusterInstall)
			// Check labels/annotations only if the resource exists in both
			for resourceType, srcFields := range srcMap {
				if dstFields, exists := dstMap[resourceType]; exists {
					dstFieldsMap, okDstFields := dstFields.(map[string]any)
					srcFieldsMap, okSrcFields := srcFields.(map[string]any)
					if !okDstFields || !okSrcFields {
						return fmt.Errorf("type mismatch for field %s: (from ProvisioningRequest: %T, from default Configmap: %T)",
							field, dstValue, srcValue)
					}

					// Override ProvisioningRequestInput's values with defaults if the label/annotation key exists in both
					for srcFieldKey, srcLabelValue := range srcFieldsMap {
						if _, exists := dstFieldsMap[srcFieldKey]; exists {
							oranUtilsLog.Info(fmt.Sprintf("%s.%s.%s found in both default configmap and clusterInstanceInput. "+
								"Overriding it in ClusterInstanceInput with value %s from the default configmap.",
								field, resourceType, srcFieldKey, srcLabelValue))
							dstFieldsMap[srcFieldKey] = srcLabelValue
						}
					}
				}
			}
		}
	}

	// Process label/annotation overrides for nodes
	dstNodes, dstExists := dstProvisioningRequestInput["nodes"]
	srcNodes, srcExists := srcConfigmap["nodes"]
	if dstExists && srcExists {
		// Determine the minimum length to merge
		minLen := len(dstNodes.([]any))
		if len(srcNodes.([]any)) < minLen {
			minLen = len(srcNodes.([]any))
		}

		for i := 0; i < minLen; i++ {
			if err := OverrideClusterInstanceLabelsOrAnnotations(
				dstNodes.([]any)[i].(map[string]any),
				srcNodes.([]any)[i].(map[string]any),
			); err != nil {
				return fmt.Errorf("type mismatch for nodes: %w", err)
			}
		}
	}

	return nil
}

func CopyK8sSecret(ctx context.Context, c client.Client, secretName, sourceNamespace, targetNamespace string) error {
	// Get the secret from the source namespace
	secret := &corev1.Secret{}
	exists, err := DoesK8SResourceExist(
		ctx, c, secretName, sourceNamespace, secret)

	// If there was an error in trying to get the secret from the source namespace, return it.
	if err != nil {
		return fmt.Errorf("error obtaining the secret %s from namespace: %s: %w", secretName, sourceNamespace, err)
	}

	if !exists {
		return fmt.Errorf("secret %s does not exist in namespace: %s", secretName, sourceNamespace)
	}

	// Modify the secret metadata to set the target namespace and remove resourceVersion
	secret.ObjectMeta.Namespace = targetNamespace
	secret.ObjectMeta.ResourceVersion = ""

	// Create the secret in the target namespace
	err = CreateK8sCR(ctx, c, secret, nil, UPDATE)
	if err != nil {
		return fmt.Errorf("failed to create secret %s in namespace %s: %w", secret.GetName(), secret.GetNamespace(), err)
	}
	return nil
}

// CheckClusterLabelsForPolicies checks if the cluster_version
// label exist for a certain ClusterInstance and returns it.
func CheckClusterLabelsForPolicies(
	clusterName string, clusterLabels map[string]string) error {

	if len(clusterLabels) == 0 {
		return NewInputError(
			"No cluster labels configured by the ClusterInstance %s(%s). "+
				"Labels are needed for cluster configuration",
			clusterName, clusterName,
		)
	}

	// Make sure the cluster-version label exists.
	_, clusterVersionLabelExists := clusterLabels[ClusterVersionLabelKey]
	if !clusterVersionLabelExists {
		return NewInputError(
			"Managed cluster %s is missing the %s label. This label is needed for correctly "+
				"generating and populating configuration data",
			clusterName, ClusterVersionLabelKey,
		)
	}
	return nil
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
		// Check if the path matches any ignored fields
		if matchesAnyPattern(diff.Path, IgnoredClusterInstanceFields) {
			// Ignored field; skip
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

// GetTLSSkipVerify returns the current requested value of the TLS Skip Verify setting
func GetTLSSkipVerify() bool {
	value, ok := os.LookupEnv(TLSSkipVerifyEnvName)
	if !ok {
		return TLSSkipVerifyDefaultValue
	}

	result, err := strconv.ParseBool(value)
	if err != nil {
		oranUtilsLog.Error(err, fmt.Sprintf("Error parsing '%s' variable value '%s'",
			TLSSkipVerifyEnvName, value))
		return TLSSkipVerifyDefaultValue
	}

	return result
}

// loadDefaultCABundles loads the default service account and ingress CA bundles.  This should only be invoked if TLS
// verification has not been disabled since the expectation is that it will only need to be disabled when testing as a
// standalone binary in which case the paths to the bundles won't be present.  Otherwise, we always expect the bundles
// to be present when running in-cluster.
func loadDefaultCABundles(config *tls.Config) error {
	config.RootCAs = x509.NewCertPool()
	if data, err := os.ReadFile(defaultBackendCABundle); err != nil {
		// This should not happen unless the binary is being tested in standalone mode in which case the developer
		// should have disabled the TLS verification which would prevent this function from being invoked.
		return fmt.Errorf("failed to read CA bundle '%s': %w", defaultBackendCABundle, err)
		// This should not happen, but if it does continue anyway
	} else {
		// This will enable accessing public facing API endpoints signed by the default ingress controller certificate
		config.RootCAs.AppendCertsFromPEM(data)
	}

	if data, err := os.ReadFile(defaultServiceCAFile); err != nil {
		return fmt.Errorf("failed to read service CA file '%s': %w", defaultServiceCAFile, err)
	} else {
		// This will enable accessing internal services signed by the service account signer.
		config.RootCAs.AppendCertsFromPEM(data)
	}

	return nil
}

// GetDefaultTLSConfig sets the TLS configuration attributes appropriately to enable communication between internal
// services and accessing the public facing API endpoints.
func GetDefaultTLSConfig(config *tls.Config) (*tls.Config, error) {
	if config == nil {
		config = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	// Allow developers to override the TLS verification
	config.InsecureSkipVerify = GetTLSSkipVerify()
	if !config.InsecureSkipVerify {
		// TLS verification is enabled therefore we need to load the CA bundles that are injected into our filesystem
		// automatically; which happens since we are defined as using a service-account
		err := loadDefaultCABundles(config)
		if err != nil {
			return nil, fmt.Errorf("error loading default CABundles: %w", err)
		}
	}

	return config, nil
}

// GetDefaultBackendTransport returns an HTTP transport with the proper TLS defaults set.
func GetDefaultBackendTransport() (http.RoundTripper, error) {
	tlsConfig, err := GetDefaultTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	if err != nil {
		return nil, err
	}

	return net.SetTransportDefaults(&http.Transport{TLSClientConfig: tlsConfig}), nil
}

// Helper function to find the matching NodeGroup by role
func FindNodeGroupByRole(role string, nodeGroups []hwv1alpha1.NodeGroup) (*hwv1alpha1.NodeGroup, error) {
	for i, group := range nodeGroups {
		if group.Name == role {
			return &nodeGroups[i], nil
		}
	}
	return nil, fmt.Errorf("node group with role %s not found", role)
}

// Help function to extract the node interfaces per role and count the nodes per group
func ProcessClusterNodeGroups(clusterInstance *siteconfig.ClusterInstance, nodeGroups []hwv1alpha1.NodeGroup, roleCounts map[string]int) error {
	// Map to keep track of processed roles and the corresponding interfaces
	processedRoles := make(map[string][]string)

	for _, node := range clusterInstance.Spec.Nodes {
		// Count the nodes per group
		roleCounts[node.Role]++

		// Find the node group corresponding to this role
		nodeGroup, err := FindNodeGroupByRole(node.Role, nodeGroups)
		if err != nil {
			return fmt.Errorf("could not find node group for role %s: %w", node.Role, err)
		}

		// Get the interface names for the current node
		var currentInterfaces []string
		for _, iface := range node.NodeNetwork.Interfaces {
			currentInterfaces = append(currentInterfaces, iface.Name)
		}

		// If the role has not been processed yet, add the interfaces
		// else check if the interfaces are the same as the first node with this role
		if _, ok := processedRoles[node.Role]; !ok {
			nodeGroup.Interfaces = currentInterfaces
			processedRoles[node.Role] = currentInterfaces
		} else if !slices.Equal(processedRoles[node.Role], currentInterfaces) {
			// Nodes with the same role and hardware profile should have identical interfaces
			return fmt.Errorf("%s has inconsistent interfaces for role %s", node.HostName, node.Role)
		}
	}

	return nil
}

// Helper function to select the boot interface based on label and return the interface MAC address
func GetBootMacAddress(interfaces []*hwv1alpha1.Interface, nodePool *hwv1alpha1.NodePool) (string, error) {
	// Get the boot interface label from annotation
	annotation := nodePool.GetAnnotations()
	if annotation == nil {
		return "", fmt.Errorf("annotations are missing from nodePool %s in namespace %s", nodePool.Name, nodePool.Namespace)
	}
	// Ensure the boot interface label annotation exists and is not empty
	bootIfaceLabel, exists := annotation[HwTemplateBootIfaceLabel]
	if !exists || bootIfaceLabel == "" {
		return "", fmt.Errorf("%s annotation is missing or empty from nodePool %s in namespace %s",
			HwTemplateBootIfaceLabel, nodePool.Name, nodePool.Namespace)
	}

	for _, iface := range interfaces {
		if iface.Label == bootIfaceLabel {
			return iface.MACAddress, nil
		}
	}
	return "", fmt.Errorf("no boot interface found")
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

// TimeoutExceeded returns true if it's been more time than the timeout configuration.
func TimeoutExceeded(provisioningRequest *provisioningv1alpha1.ProvisioningRequest) bool {
	timeSince := time.Since(provisioningRequest.Status.ClusterDetails.NonCompliantAt.Time)
	timeout := time.Duration(provisioningRequest.Spec.Timeout.Configuration) * time.Minute
	return timeSince > timeout
}

// GetEnvOrDefault returns the value of the named environment variable or the supplied default value if the environment
// variable is not set.
func GetEnvOrDefault(name, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	return value
}

// GetHwMgrPluginNS returns the value of environment variable HWMGR_PLUGIN_NAMESPACE
func GetHwMgrPluginNS() string {
	// Ensure that this code only runs once
	once.Do(func() {
		hwMgrPluginNameSpace = GetEnvOrDefault(HwMgrPluginNameSpace, DefaultPluginNamespace)
	})
	return hwMgrPluginNameSpace
}

// SetCloudManagerGenerationStatus sets the CloudManager's ObservedGeneration on the node pool resource status field
func SetCloudManagerGenerationStatus(ctx context.Context, c client.Client, nodePool *hwv1alpha1.NodePool) error {
	// Get the generated NodePool and its metadata.generation
	exists, err := DoesK8SResourceExist(ctx, c, nodePool.GetName(),
		nodePool.GetNamespace(), nodePool)
	if err != nil {
		return fmt.Errorf("failed to get NodePool %s in namespace %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
	}
	if !exists {
		return fmt.Errorf("nodePool %s does not exist in namespace %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
	}
	// We only set ObservedGeneration when the NodePool is first created because we do not update the spec after creation.
	// Once ObservedGeneration is set, no need to update it again.
	if nodePool.Status.CloudManager.ObservedGeneration != 0 {
		// ObservedGeneration is already set, so we do nothing.
		return nil
	}
	// Set ObservedGeneration to the current generation of the resource
	nodePool.Status.CloudManager.ObservedGeneration = nodePool.ObjectMeta.Generation
	err = UpdateK8sCRStatus(ctx, c, nodePool)
	if err != nil {
		return fmt.Errorf("failed to update status for NodePool %s %s: %w", nodePool.GetName(), nodePool.GetNamespace(), err)
	}
	return nil
}

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
		return subSchema, fmt.Errorf("non compliant Main Schema, missing properties: %w", err)
	}
	properties, ok := jsonObject[PropertiesString].(map[string]any)
	if !ok {
		return subSchema, fmt.Errorf("could not cast properties as map[string]any: %w", err)
	}

	subSchemaValue, ok := properties[subSchemaKey]
	if !ok {
		return subSchema, fmt.Errorf("subSchema %s does not exist: %w", subSchemaKey, err)
	}

	subSchema, ok = subSchemaValue.(map[string]any)
	if !ok {
		return subSchema, fmt.Errorf("subSchema %s is not a valid map: %w", subSchemaKey, err)
	}
	return subSchema, nil
}

// ExtractMatchingInput extracts the portion of the input data that corresponds to a given subSchema key.
func ExtractMatchingInput(input []byte, subSchemaKey string) (any, error) {
	inputData := make(map[string]any)
	err := json.Unmarshal(input, &inputData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal input: %w", err)
	}

	// Check if the input contains the subSchema key
	matchingInput, ok := inputData[subSchemaKey]
	if !ok {
		return nil, fmt.Errorf("input does not contain key %s: %w", subSchemaKey, err)
	}
	return matchingInput, nil
}

// MapKeysToSlice takes a map[string]bool and returns a slice of strings containing the keys
func MapKeysToSlice(inputMap map[string]bool) []string {
	keys := make([]string, 0, len(inputMap))
	for key := range inputMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// OAuthClientConfig defines the parameters required to establish an HTTP Client capable of acquiring an OAuth Token
// from an OAuth capable authorization server.
type OAuthClientConfig struct {
	// Defines a PEM encoded set of CA certificates used to validate server certificates.  If not provided then the
	// default root CA bundle will be used.
	CaBundle []byte
	// Defines the OAuth client-id attribute to be used when acquiring a token.  If not provided (for debug/testing)
	// then a normal HTTP client without OAuth capabilities will be created
	ClientId     string
	ClientSecret string
	// The absolute URL of the API endpoint to be used to acquire a token
	// (e.g., http://example.com/realms/oran/protocol/openid-connect/token)
	TokenUrl string
	// The list of OAuth scopes requested by the client.  These will be dictated by what the SMO is expecting to see in
	// the token.
	Scopes []string
}

// SetupOAuthClient creates an HTTP client capable of acquiring an OAuth token used to authorize client requests.  If
// the config excludes the OAuth specific sections then the client produced is a simple HTTP client without OAuth
// capabilities.
func SetupOAuthClient(ctx context.Context, config OAuthClientConfig) (*http.Client, error) {
	tlsConfig, _ := GetDefaultTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})

	if len(config.CaBundle) != 0 {
		// If the user has provided a CA bundle them we must use it to build our client so that we can verify the
		// identity of remote servers.
		if tlsConfig.RootCAs == nil {
			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(config.CaBundle) {
				return nil, fmt.Errorf("failed to append certificate bundle to pool")
			}
			tlsConfig.RootCAs = certPool
		} else {
			// We may not need the default CA bundles in this case but there's no harm in keeping them in the pool
			// to handle cases where they may be needed.
			tlsConfig.RootCAs.AppendCertsFromPEM(config.CaBundle)
		}
	}

	c := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig}}

	if config.ClientId != "" {
		config := clientcredentials.Config{
			ClientID:       config.ClientId,
			ClientSecret:   config.ClientSecret,
			TokenURL:       config.TokenUrl,
			Scopes:         config.Scopes,
			EndpointParams: nil,
			AuthStyle:      oauth2.AuthStyleInParams,
		}

		ctx = context.WithValue(ctx, oauth2.HTTPClient, c)

		c = config.Client(ctx)
	}

	return c, nil
}

// GetClusterId retrieves the UUID value for the cluster specified by name
func GetClusterId(ctx context.Context, c client.Client, name string) (string, error) {
	object := &openshiftv1.ClusterVersion{}

	err := c.Get(ctx, types.NamespacedName{Name: name}, object)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve ClusterVersion '%s', error: %w", name, err)
	} else {
		return string(object.Spec.ClusterID), nil
	}
}

package utils

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/util/net"
	ctrl "sigs.k8s.io/controller-runtime"

	sprig "github.com/go-task/slim-sprig/v3"
	"github.com/xeipuuv/gojsonschema"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/files"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

var oranUtilsLog = ctrl.Log.WithName("oranUtilsLog")

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

func ValidateJsonAgainstJsonSchema(schema, input string) error {
	schemaLoader := gojsonschema.NewStringLoader(schema)
	inputLoader := gojsonschema.NewStringLoader(input)

	result, err := gojsonschema.Validate(schemaLoader, inputLoader)
	if err != nil {
		oranUtilsLog.Error(err, "Error validating the input against the schema")
		return fmt.Errorf("failed when validating the input against the schema: %w", err)
	}

	if result.Valid() {
		return nil
	} else {
		errorDescription := ""
		for _, description := range result.Errors() {
			errorDescription = errorDescription + " " + description.String()
		}

		return fmt.Errorf(
			fmt.Sprintf("The input does not match the schema: %s", errorDescription))
	}
}

func GetBMCDetailsForClusterInstance(node map[string]interface{}, clusterRequest string) (
	string, string, string, error) {
	// Get the BMC details.
	bmcCredentialsDetailsInterface, bmcCredentialsDetailsExist := node["bmcCredentialsDetails"]

	if !bmcCredentialsDetailsExist {
		return "", "", "", NewInputError(
			`\"bmcCredentialsDetails\" key expected to exist in ClusterTemplateInput 
			of ClusterRequest %s, but it's missing`,
			clusterRequest,
		)
	}

	bmcCredentialsDetails := bmcCredentialsDetailsInterface.(map[string]interface{})

	// Get the BMC username and password.
	username, usernameExists := bmcCredentialsDetails["username"].(string)
	if !usernameExists {
		return "", "", "", NewInputError(
			`\"bmcCredentialsDetails.username\" key expected to exist in ClusterTemplateInput 
			of ClusterRequest %s, but it's missing`,
			clusterRequest,
		)
	}

	password, passwordExists := bmcCredentialsDetails["password"].(string)
	if !passwordExists {
		return "", "", "", NewInputError(
			`\"bmcCredentialsDetails.password\" key expected to exist in ClusterTemplateInput 
			of ClusterRequest %s, but it's missing`,
			clusterRequest,
		)
	}

	secretName := ""
	// Get the BMC CredentialsName.
	bmcCredentialsNameInterface, bmcCredentialsNameExist := node["bmcCredentialsName"]
	if !bmcCredentialsNameExist {
		nodeHostnameInterface, nodeHostnameExists := node["hostName"]
		if !nodeHostnameExists {
			secretName = clusterRequest
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
	oranUtilsLog.Info("[CreateK8sCR] Resource", "name", key.Name)

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

func GetDeploymentVolumes(serverName string) []corev1.Volume {
	if serverName == ORANO2IMSMetadataServerName || serverName == ORANO2IMSResourceServerName {
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

	if serverName == ORANO2IMSDeploymentManagerServerName {
		return []corev1.Volume{
			{
				Name: "tls",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: fmt.Sprintf("%s-tls", serverName),
					},
				},
			},
			{
				Name: "authz",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "authz"},
					},
				},
			},
		}
	}

	return []corev1.Volume{}
}

func GetDeploymentVolumeMounts(serverName string) []corev1.VolumeMount {
	if serverName == ORANO2IMSMetadataServerName || serverName == ORANO2IMSResourceServerName {
		return []corev1.VolumeMount{
			{
				Name:      "tls",
				MountPath: "/secrets/tls",
			},
		}
	}

	if serverName == ORANO2IMSDeploymentManagerServerName {
		return []corev1.VolumeMount{
			{
				Name:      "tls",
				MountPath: "/secrets/tls",
			},
			{
				Name:      "authz",
				MountPath: "/configmaps/authz",
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
func getSearchAPI(ctx context.Context, c client.Client, orano2ims *oranv1alpha1.ORANO2IMS) (string, error) {
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
	ingressSplit := strings.Split(orano2ims.Spec.IngressHost, ".apps")
	if len(ingressSplit) != 2 {
		return "", fmt.Errorf("the searchAPIBackendURL could not be obtained from the IngressHost. " +
			"Directly specify the searchAPIBackendURL in the ORANO2IMS CR or update the IngressHost")
	}
	domain := ".apps" + ingressSplit[len(ingressSplit)-1]

	// The searchAPI is obtained from the "search-api" string and the ACM namespace.
	searchAPI := "https://" + "search-api-" + acmNamespace + domain

	return searchAPI, nil
}

func GetServerArgs(ctx context.Context, c client.Client,
	orano2ims *oranv1alpha1.ORANO2IMS,
	serverName string) (result []string, err error) {
	// MetadataServer
	if serverName == ORANO2IMSMetadataServerName {
		result = slices.Clone(MetadataServerArgs)
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--external-address=https://%s", orano2ims.Spec.IngressHost))

		return
	}

	// ResourceServer
	if serverName == ORANO2IMSResourceServerName {
		searchAPI := orano2ims.Spec.ResourceServerConfig.BackendURL
		if searchAPI == "" {
			searchAPI, err = getSearchAPI(ctx, c, orano2ims)
			if err != nil {
				return nil, err
			}
		}

		result = slices.Clone(ResourceServerArgs)

		// Add the cloud-id, backend-url, and token args:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", searchAPI),
			GetBackendTokenArg(orano2ims.Spec.ResourceServerConfig.BackendToken))

		return result, nil
	}

	// DeploymentManagerServer
	if serverName == ORANO2IMSDeploymentManagerServerName {
		result = slices.Clone(DeploymentManagerServerArgs)

		// Set the cloud identifier:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
		)

		// Set the backend type:
		if orano2ims.Spec.DeploymentManagerServerConfig.BackendType != "" {
			result = append(
				result,
				fmt.Sprintf("--backend-type=%s", orano2ims.Spec.DeploymentManagerServerConfig.BackendType),
			)
		}

		// If no backend URL has been provided then use the default URL of the Kubernetes
		// API server of the cluster:
		backendURL := orano2ims.Spec.DeploymentManagerServerConfig.BackendURL
		if backendURL == "" {
			backendURL = defaultBackendURL
		}

		// Add the backend and token args:
		result = append(
			result,
			fmt.Sprintf("--backend-url=%s", backendURL),
			GetBackendTokenArg(orano2ims.Spec.DeploymentManagerServerConfig.BackendToken))

		// Add the extensions:
		extensionsArgsArray := extensionsToExtensionArgs(orano2ims.Spec.DeploymentManagerServerConfig.Extensions)
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

	err = yaml.Unmarshal(output.Bytes(), &renderedTemplate.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal, err: %w", err)
	}

	return renderedTemplate, nil
}

// toYaml converts an interface to a YAML string and trims the trailing newline
func toYaml(v interface{}) (string, error) {
	// yaml.Marshal adds a trailing newline to its output
	yamlData, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("failed to marshal interface to YAML: %w", err)
	}

	return strings.TrimRight(string(yamlData), "\n"), nil
}

// extractBeforeDot returns the strubstring before the first dot.
func extractBeforeDot(s string) string {
	dotIndex := strings.Index(s, ".")
	if dotIndex == -1 {
		return s
	}
	return s[:dotIndex]
}

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
			"the ConfigMap %s is not found in the namespace %s", name, namespace)
	}
	return existingConfigmap, nil
}

// ExtractTemplateDataFromConfigMap extractes the template data associated with the specified key
// from the provided ConfigMap. The data is expected to be in YAML format.
func ExtractTemplateDataFromConfigMap[T any](ctx context.Context, c client.Client, cm *corev1.ConfigMap, expectedKey string) (T, error) {
	var validData T

	// Find the expected key is present in the configmap data
	defaults, exists := cm.Data[expectedKey]
	if !exists {
		return validData, NewInputError(
			"the expected key %s does not exist in the ConfigMap %s data", expectedKey, cm.GetName())
	}

	// Parse the YAML data into a map
	err := yaml.Unmarshal([]byte(defaults), &validData)
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
	spec map[string]interface{}, clusterName string) error {

	labelsInterface, labelsExists := spec["clusterLabels"]

	if !labelsExists {
		return NewInputError(
			"No cluster labels configured by the ClusterInstance %s(%s). "+
				"Labels are needed for cluster configuration",
			clusterName, clusterName,
		)
	}

	// Make sure the cluster-version label exists.
	_, clusterVersionLabelExists :=
		labelsInterface.(map[string]interface{})[ClusterVersionLabelKey]
	if !clusterVersionLabelExists {
		return NewInputError(
			"Managed cluster %s is missing the %s label. This label is needed for correctly "+
				"generating and populating configuration data",
			clusterName, ClusterVersionLabelKey,
		)
	}

	return nil
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

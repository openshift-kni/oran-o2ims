package utils

import (
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

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"k8s.io/apimachinery/pkg/util/net"
	ctrl "sigs.k8s.io/controller-runtime"

	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	openshiftv1 "github.com/openshift/api/config/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

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
	// The client certificate to be used when initiating connection to the server.
	ClientCert *tls.Certificate
}

const (
	PropertiesString = "properties"
	requiredString   = "required"
)

var (
	oranUtilsLog = ctrl.Log.WithName("oranUtilsLog")
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

// CreateK8sCR creates/updates/patches an object.
func CreateK8sCR(ctx context.Context, c client.Client,
	newObject client.Object, ownerObject client.Object,
	operation string) (err error) {

	// Get the name and namespace of the object:
	key := client.ObjectKeyFromObject(newObject)

	// We can set the owner reference only for objects that live in the same namespace, as cross
	// namespace owners are forbidden. This also applies to non-namespaced objects like cluster
	// roles or cluster role bindings; those have empty namespaces, so the equals comparison
	// should also work.
	if ownerObject != nil && (ownerObject.GetNamespace() == key.Namespace || ownerObject.GetNamespace() == "") {
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
			if err := c.Patch(ctx, newObject, client.MergeFrom(oldObject)); err != nil {
				return fmt.Errorf("failed to patch object %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
			}
			return nil
		} else if operation == UPDATE {
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
			return false, nil
		} else {
			return false, fmt.Errorf("failed to check existence of resource '%s' in namespace '%s': %w", name, namespace, err)
		}
	} else {
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

// GetIngressDomain will determine the network domain of the default ingress controller
func GetIngressDomain(ctx context.Context, c client.Client) (string, error) {
	ingressController := &unstructured.Unstructured{}
	ingressController.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "operator.openshift.io",
		Kind:    "IngressController",
		Version: "v1",
	})

	err := c.Get(ctx, client.ObjectKey{
		Name:      "default",
		Namespace: "openshift-ingress-operator",
	}, ingressController)

	if err != nil {
		oranUtilsLog.Info(fmt.Sprintf("[getIngressDomain] default ingress controller object not found, error: %s", err))
		return "", fmt.Errorf("default ingress controller object not found: %w", err)
	}

	status := ingressController.Object["status"].(map[string]interface{})
	domain, ok := status["domain"]

	if ok {
		return domain.(string), nil
	}

	return "", fmt.Errorf("default ingress controller does not have expected 'status.domain' attribute")
}

func GetServerArgs(inventory *inventoryv1alpha1.Inventory, serverName string) (result []string, err error) {
	cloudId := DefaultOCloudID
	if inventory.Spec.CloudID != nil {
		cloudId = *inventory.Spec.CloudID
	}

	// MetadataServer
	if serverName == InventoryMetadataServerName {
		result = slices.Clone(MetadataServerArgs)
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Status.ClusterID),
			fmt.Sprintf("--global-cloud-id=%s", cloudId),
			fmt.Sprintf("--external-address=https://%s", inventory.Status.IngressHost))

		return
	}

	// ResourceServer
	if serverName == InventoryResourceServerName {
		searchAPI := inventory.Spec.ResourceServerConfig.BackendURL
		if searchAPI == "" {
			searchAPI = defaultSearchApiURL
		}

		result = slices.Clone(ResourceServerArgs)

		// Add the cloud-id, backend-url, and token args:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Status.ClusterID),
			fmt.Sprintf("--backend-url=%s", searchAPI),
			fmt.Sprintf("--global-cloud-id=%s", cloudId),
			fmt.Sprintf("--namespace=%s", inventory.Namespace),
			GetBackendTokenArg(inventory.Spec.ResourceServerConfig.BackendToken))

		return result, nil
	}

	// DeploymentManagerServer
	if serverName == InventoryDeploymentManagerServerName {
		result = slices.Clone(DeploymentManagerServerArgs)

		// Set the cloud identifier:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Status.ClusterID),
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
			backendURL = defaultApiServerURL
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

// ExtractBeforeDot returns the strubstring before the first dot.
func ExtractBeforeDot(s string) string {
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
func ExtractTemplateDataFromConfigMap[T any](cm *corev1.ConfigMap, expectedKey string) (T, error) {
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

// GetEnvOrDefault returns the value of the named environment variable or the supplied default value if the environment
// variable is not set.
func GetEnvOrDefault(name, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	return value
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

// SetupOAuthClient creates an HTTP client capable of acquiring an OAuth token used to authorize client requests.  If
// the config excludes the OAuth specific sections then the client produced is a simple HTTP client without OAuth
// capabilities.
func SetupOAuthClient(ctx context.Context, config OAuthClientConfig) (*http.Client, error) {
	tlsConfig, _ := GetDefaultTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})

	if config.ClientCert != nil {
		// Enable mTLS if a client certificate was provided.  The client CA is expected to be recognized by the server.
		tlsConfig.Certificates = []tls.Certificate{*config.ClientCert}
	}

	if len(config.CaBundle) != 0 {
		// If the user has provided a CA bundle then we must use it to build our client so that we can verify the
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

// GetClusterID retrieves the UUID value for the cluster specified by name
func GetClusterID(ctx context.Context, c client.Client, name string) (string, error) {
	object := &openshiftv1.ClusterVersion{}

	err := c.Get(ctx, types.NamespacedName{Name: name}, object)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve ClusterVersion '%s', error: %w", name, err)
	} else {
		return string(object.Spec.ClusterID), nil
	}
}

func GetIBGUFromUpgradeDefaultsConfigmap(
	ctx context.Context,
	c client.Client,
	cmName string,
	cmNamespace string,
	cmKey string,
	clusterName string,
	ibguName string,
	ibguNamespace string,
) (*ibguv1alpha1.ImageBasedGroupUpgrade, error) {

	existingConfigmap, err := GetConfigmap(ctx, c, cmName, cmNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigmapReference: %w", err)
	}
	defaults, err := GetConfigMapField(existingConfigmap, UpgradeDefaultsConfigmapKey)

	if err != nil {
		return nil, fmt.Errorf("failed to get Configmap Field: %w", err)
	}
	out, err := k8syaml.ToJSON([]byte(defaults))
	if err != nil {
		return nil, fmt.Errorf("failed to convert confimap data to JSON: %w", err)
	}

	ibguSpec := &ibguv1alpha1.ImageBasedGroupUpgradeSpec{}
	err = json.Unmarshal(out, &ibguSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to convert confimap data to IBGU spec: %w", err)
	}
	ibguSpec.ClusterLabelSelectors = []metav1.LabelSelector{
		{
			MatchLabels: map[string]string{
				"name": clusterName,
			},
		},
	}

	return &ibguv1alpha1.ImageBasedGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ibguName,
			Namespace: ibguNamespace,
		},
		Spec: *ibguSpec,
	}, nil
}

// GetCertFromSecret retrieves an X.509 certificate from a Secret
func GetCertFromSecret(ctx context.Context, c client.Client, name, namespace string) (*tls.Certificate, error) {
	secret, err := GetSecret(ctx, c, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve secret '%s': %w", name, err)
	}

	certBytes, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, NewInputError("secret '%s' does not contain key 'tls.crt'", name)
	}

	keyBytes, ok := secret.Data["tls.key"]
	if !ok {
		return nil, NewInputError("secret '%s' does not contain key 'tls.key'", name)
	}

	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate from secret '%s': %w", name, err)
	}

	return &cert, nil
}

// CreateDefaultInventoryCR creates the default Inventory CR so that the system has running servers
func CreateDefaultInventoryCR(ctx context.Context, c client.Client) error {
	inventory := inventoryv1alpha1.Inventory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultInventoryCR,
			Namespace: GetEnvOrDefault(DefaultNamespaceEnvName, DefaultNamespace),
		},
		Spec: inventoryv1alpha1.InventorySpec{
			AlarmSubscriptionServerConfig: inventoryv1alpha1.AlarmSubscriptionServerConfig{
				ServerConfig: inventoryv1alpha1.ServerConfig{
					Enabled: false},
			},
			DeploymentManagerServerConfig: inventoryv1alpha1.DeploymentManagerServerConfig{
				ServerConfig: inventoryv1alpha1.ServerConfig{
					Enabled: true,
				},
			},
			MetadataServerConfig: inventoryv1alpha1.MetadataServerConfig{
				ServerConfig: inventoryv1alpha1.ServerConfig{
					Enabled: true,
				},
			},
			ResourceServerConfig: inventoryv1alpha1.ResourceServerConfig{
				ServerConfig: inventoryv1alpha1.ServerConfig{
					Enabled: true,
				},
			},
		},
	}

	err := c.Create(ctx, &inventory)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create default inventory CR: %w", err)
		}
	}

	return nil
}

// GetRoleToGroupNameMap creates a mapping of Role to Group Name from NodePool
func GetRoleToGroupNameMap(nodePool *hwv1alpha1.NodePool) map[string]string {
	roleToNodeGroupName := make(map[string]string)
	for _, nodeGroup := range nodePool.Spec.NodeGroup {

		if _, exists := roleToNodeGroupName[nodeGroup.Role]; !exists {
			roleToNodeGroupName[nodeGroup.Role] = nodeGroup.Name
		}
	}
	return roleToNodeGroupName
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	ctrl "sigs.k8s.io/controller-runtime"

	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
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

const (
	PropertiesString = "properties"
	requiredString   = "required"
)

var (
	oranUtilsLog = ctrl.Log.WithName("oranUtilsLog")
)

func UpdateK8sCRStatus(ctx context.Context, c client.Client, object client.Object) error {
	cr, ok := object.(*provisioningv1alpha1.ProvisioningRequest)
	if ok {
		cr.Status.ObservedGeneration = cr.Generation
	}

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

// HasApiEndpoints determines whether a server exposes a set of API endpoints
func HasApiEndpoints(serverName string) bool {
	return serverName == InventoryDatabaseServerName ||
		serverName == InventoryClusterServerName ||
		serverName == InventoryAlarmServerName ||
		serverName == InventoryResourceServerName ||
		serverName == InventoryArtifactsServerName ||
		serverName == InventoryProvisioningServerName ||
		serverName == HardwarePluginManagerServerName ||
		serverName == LoopbackPluginServerName ||
		serverName == Metal3PluginServerName
}

// HasDatabase determines whether a server owns a logical database instance
func HasDatabase(serverName string) bool {
	return serverName == InventoryResourceServerName ||
		serverName == InventoryClusterServerName ||
		serverName == InventoryAlarmServerName
}

// RequiresInternalListener determines whether a server expects its API to be accessed by another server.  If this
// is the case, then in an OAuth configuration we run a second listener for that server which handles authenticating
// using a Kubernetes service account token rather than an OAuth token.
func RequiresInternalListener(serverName string) bool {
	return serverName == InventoryResourceServerName ||
		serverName == InventoryClusterServerName ||
		serverName == InventoryAlarmServerName ||
		serverName == HardwarePluginManagerServerName ||
		serverName == LoopbackPluginServerName ||
		serverName == Metal3PluginServerName
}

// IsOAuthEnabled determines if the Inventory CR has OAuth attributes provided.
func IsOAuthEnabled(inventory *inventoryv1alpha1.Inventory) bool {
	return inventory.Spec.SmoConfig != nil && inventory.Spec.SmoConfig.OAuthConfig != nil
}

// NeedsOAuthAccess determines whether a server requires access to the Authorization server.  This can be either
// because it needs to get a token to communicate with the SMO or to validate a token against the authorization server
// directly.
func NeedsOAuthAccess(serverName string) bool {
	return serverName == InventoryResourceServerName ||
		serverName == InventoryClusterServerName ||
		serverName == InventoryAlarmServerName ||
		serverName == InventoryArtifactsServerName ||
		serverName == InventoryProvisioningServerName ||
		serverName == HardwarePluginManagerServerName ||
		serverName == LoopbackPluginServerName ||
		serverName == Metal3PluginServerName
}

// getTLSClientCertificateSecret determines whether there is a TLS secret configured.
func getTLSClientCertificateSecret(inventory *inventoryv1alpha1.Inventory) *string {
	if inventory.Spec.SmoConfig == nil || inventory.Spec.SmoConfig.TLS == nil {
		return nil
	}

	tlsConfig := inventory.Spec.SmoConfig.TLS
	return tlsConfig.SecretName
}

// GetDeploymentVolumes builds the list of volumes applicable to the specified server
func GetDeploymentVolumes(serverName string, inventory *inventoryv1alpha1.Inventory) []corev1.Volume {
	var volumes []corev1.Volume
	tlsDefaultMode := int32(os.FileMode(0o400))
	if HasApiEndpoints(serverName) {
		volumes = append(volumes, []corev1.Volume{
			{
				Name: "tls",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						DefaultMode: &tlsDefaultMode,
						SecretName:  fmt.Sprintf("%s-tls", serverName),
					},
				},
			},
		}...)
	}

	if NeedsOAuthAccess(serverName) {
		if inventory.Spec.SmoConfig != nil {
			clientSecretName := getTLSClientCertificateSecret(inventory)
			if clientSecretName != nil {
				volumes = append(volumes, corev1.Volume{
					Name: "smo-mtls",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							DefaultMode: &tlsDefaultMode,
							SecretName:  *clientSecretName,
						},
					},
				})
			}
		}
		if inventory.Spec.CaBundleName != nil {
			volumes = append(volumes, corev1.Volume{
				Name: "ca-bundle",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: *inventory.Spec.CaBundleName,
						},
					},
				},
			})
		}
	}

	return volumes
}

// GetDeploymentVolumeMounts builds the list of volume mounts applicable to the specified server
func GetDeploymentVolumeMounts(serverName string, inventory *inventoryv1alpha1.Inventory) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount
	if HasApiEndpoints(serverName) {
		mounts = append(mounts, []corev1.VolumeMount{
			{
				Name:      "tls",
				MountPath: TLSServerMountPath,
			},
		}...)
	}

	if NeedsOAuthAccess(serverName) {
		if inventory.Spec.SmoConfig != nil {
			clientSecretName := getTLSClientCertificateSecret(inventory)
			if clientSecretName != nil {
				mounts = append(mounts, corev1.VolumeMount{
					Name:      "smo-mtls",
					MountPath: TLSClientMountPath,
				})
			}
		}
		if inventory.Spec.CaBundleName != nil {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      "ca-bundle",
				MountPath: CABundleMountPath,
			})
		}
	}

	return mounts
}

func GetBackendTokenArg(backendToken string) string {
	// If no backend token has been provided then use the token of the service account
	// that will eventually execute the server. Note that the file may not exist,
	// but we can't check it here as that will be a different pod.
	if backendToken != "" {
		return fmt.Sprintf("--backend-token=%s", backendToken)
	}

	return fmt.Sprintf("--backend-token-file=%s", DefaultBackendTokenFile)
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

// GetSearchAPI attempts to find the search-api service using its label selector
func GetSearchAPI(ctx context.Context, c client.Client) (*corev1.Service, error) {
	// Build the search criteria
	selector := labels.NewSelector()
	monitorSelector, err := labels.NewRequirement(SearchApiLabelKey, selection.Equals, []string{SearchApiLabelValue})
	if err != nil {
		return nil, fmt.Errorf("failed to create search-api monitor selector: %w", err)
	}

	// Do the search
	services := &corev1.ServiceList{}
	err = c.List(ctx, services, &client.ListOptions{
		LabelSelector: selector.Add(*monitorSelector),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	if len(services.Items) == 0 {
		return nil, fmt.Errorf("failed to list services: no services found")
	}

	// Should only be 1 result therefore return the first item
	return &services.Items[0], nil
}

// GetSearchURL attempts to build the Search API service URL by dynamically looking up the service.
func GetSearchURL(ctx context.Context, c client.Client) (string, error) {
	service, err := GetSearchAPI(ctx, c)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", service.Name, service.Namespace, service.Spec.Ports[0].Port), nil
}

// GetServerDatabasePasswordName retrieves name of the environment variable used to store the server's database password
func GetServerDatabasePasswordName(serverName string) (string, error) {
	switch serverName {
	case InventoryAlarmServerName:
		return AlarmsPasswordEnvName, nil
	case InventoryResourceServerName:
		return ResourcesPasswordEnvName, nil
	case InventoryClusterServerName:
		return ClustersPasswordEnvName, nil
	default:
		return "", fmt.Errorf("database name not found for server '%s'", serverName)
	}
}

// addArgsForOAuth sets up the command line arguments related to enabling communication to the SMO and OAuth server
func addArgsForOAuth(inventory *inventoryv1alpha1.Inventory, args []string) []string {
	if inventory.Spec.SmoConfig != nil {
		smo := inventory.Spec.SmoConfig

		if smo.OAuthConfig != nil {
			args = append(args,
				fmt.Sprintf("--oauth-scopes=%s", strings.Join(smo.OAuthConfig.Scopes, ",")),
				fmt.Sprintf("--oauth-issuer-url=%s", smo.OAuthConfig.URL),
				fmt.Sprintf("--oauth-token-endpoint=%s", smo.OAuthConfig.TokenEndpoint),
				fmt.Sprintf("--oauth-username-claim=%s", smo.OAuthConfig.UsernameClaim),
				fmt.Sprintf("--oauth-groups-claim=%s", smo.OAuthConfig.GroupsClaim),
				fmt.Sprintf("--oauth-client-binding-claim=%s", smo.OAuthConfig.ClientBindingClaim))
		}

		if smo.TLS != nil && smo.TLS.SecretName != nil {
			args = append(args,
				fmt.Sprintf("--tls-client-cert=%s/tls.crt", TLSClientMountPath),
				fmt.Sprintf("--tls-client-key=%s/tls.key", TLSClientMountPath),
			)
		}
	}

	if inventory.Spec.CaBundleName != nil {
		args = append(args,
			fmt.Sprintf("--ca-bundle-file=%s/%s", CABundleMountPath, CABundleFilename),
		)
	}

	return args
}

func GetServerArgs(inventory *inventoryv1alpha1.Inventory, serverName string) (result []string, err error) {
	cloudId := DefaultOCloudID
	if inventory.Spec.CloudID != nil {
		cloudId = *inventory.Spec.CloudID
	}

	// AlarmServer
	if serverName == InventoryAlarmServerName {
		result = slices.Clone(AlarmServerArgs)
		result = append(
			result,
			fmt.Sprintf("--global-cloud-id=%s", cloudId))

		// Add OAuth command line arguments
		result = addArgsForOAuth(inventory, result)
		return
	}

	if serverName == InventoryArtifactsServerName {
		result = slices.Clone(ArtifactsServerArgs)

		// Add OAuth command line arguments
		result = addArgsForOAuth(inventory, result)
		return
	}

	// ResourceServer
	if serverName == InventoryResourceServerName {
		result = slices.Clone(ResourceServerArgs)

		// Add the cloud-id, backend-url, and token args:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Status.ClusterID),
			fmt.Sprintf("--global-cloud-id=%s", cloudId),
			fmt.Sprintf("--external-address=https://%s", inventory.Status.IngressHost))

		// Add OAuth command line arguments
		result = addArgsForOAuth(inventory, result)

		return result, nil
	}

	// ClusterServer
	if serverName == InventoryClusterServerName {
		result = slices.Clone(ClusterServerArgs)
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", inventory.Status.ClusterID))

		// Add OAuth command line arguments
		result = addArgsForOAuth(inventory, result)

		return
	}

	// ProvisioningServer
	if serverName == InventoryProvisioningServerName {
		result = slices.Clone(ProvisioningServerArgs)

		// Add OAuth command line arguments
		result = addArgsForOAuth(inventory, result)
		return
	}

	// HwMgr Plugin Controller
	if serverName == HardwarePluginManagerServerName {
		result = slices.Clone(HardwarePluginManagerArgs)
		return
	}

	// Loopback Plugin Server
	if serverName == LoopbackPluginServerName {
		result = slices.Clone(LoopbackPluginServerArgs)
		return
	}

	// Metal3 Hardware Plugin Server
	if serverName == Metal3PluginServerName {
		result = slices.Clone(Metal3PluginServerArgs)
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

// GetDefaultsFromConfigMap returns the data of a defaults ConfigMap with its content
// separated in 2 sections:
//   - immutable: the values for configuration that is not exposed through the ClusterTemplate.
//   - editable : the values for configuration is exposed through the ClusterTemplate and
//     can later be changed through the ProvisioningRequest.
//
// If any error is encountered, the default data is returned as it is in the ConfigMap, without
// any further separation.
func GetDefaultsFromConfigMap(ctx context.Context, c client.Client, configMapName string, configMapNamespace string,
	configMapKey string, schema []byte, schemaKey string) (map[string]interface{}, error) {

	// Retrieve the configmap that holds the default data.
	defaultConfigMap, err := GetConfigmap(ctx, c, configMapName, configMapNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", configMapName, configMapNamespace, err)
	}
	defaultValues, err := ExtractTemplateDataFromConfigMap[map[string]any](defaultConfigMap, configMapKey)
	if err != nil {
		return nil, fmt.Errorf(
			"could not obtain the requested field from default ConfigMap: %w", err,
		)
	}
	// Get the schema to check the default values against it.
	subSchema, err := provisioningv1alpha1.ExtractSubSchema(schema, schemaKey)
	if err != nil {
		return nil, fmt.Errorf("could not extract subSchema: %w", err)
	}
	// Get the immutable and editable values from the defaults.
	editableValues, immutableValues, err := GetDefaultsFromMaps(
		defaultValues, subSchema["properties"].(map[string]any))
	// Build the final response. If an error occurred, return all the default ConfigMap, without separating
	// the immutable and editable values.
	resultMap := defaultValues
	if err == nil {
		resultMap = map[string]interface{}{"editable": editableValues, "immutable": immutableValues}
	}
	return resultMap, nil
}

// GetDefaultsFromMaps separates the values from a map
// into 2 maps: one with elements that match the passed schema and one with the
// elements that do not match the schema.
func GetDefaultsFromMaps[K comparable, V any](
	mapDefaults map[K]V, mapSchema map[K]V) (map[K]V, map[K]V, error) {
	editable := make(map[K]V)
	immutable := make(map[K]V)
	// Go through all the default values and check which keys are present in the schema.
	for key, defaultValue := range mapDefaults {
		if schemaValue, exists := mapSchema[key]; exists {
			switch valueType := any(defaultValue).(type) {
			case map[K]V:
				newSchema := any(schemaValue).(map[string]interface{})["properties"]
				// If no "properties" are found, consider the whole object editable.
				if newSchema == nil {
					editable[key] = defaultValue
					continue
				}
				schemaValueType := newSchema.(map[K]V)
				newEditable, newImmutable, err := GetDefaultsFromMaps(valueType, schemaValueType)
				if err != nil {
					return nil, nil, err
				}
				if len(newEditable) != 0 {
					editable[key] = any(newEditable).(V)
				}
				if len(newImmutable) != 0 {
					immutable[key] = any(newImmutable).(V)
				}
			case []V:
				newSchemaItems, ok := any(schemaValue).(map[string]interface{})["items"]
				if !ok {
					return nil, nil, fmt.Errorf("array type schema is missing its expected \"items\" component")
				}
				// We will be missing the "properties" component for an array of simple objects:
				//   apiVIPs:
				//     items:
				//       type: string
				//     maxItems: 2
				//     type: array
				newSchema := newSchemaItems.(map[string]interface{})["properties"]
				schemaValueType := newSchemaItems.(map[K]V)
				if newSchema != nil {
					schemaValueType = newSchema.(map[K]V)
				}
				newEditable, newImmutable, err := GetDefaultsFromSlices[K](valueType, schemaValueType)
				if err != nil {
					return nil, nil, err
				}
				if len(newEditable) != 0 {
					editable[key] = any(newEditable).(V)
				}
				if len(newImmutable) != 0 {
					immutable[key] = any(newImmutable).(V)
				}
			default:
				editable[key] = defaultValue
			}
		} else {
			immutable[key] = defaultValue
		}
	}
	return editable, immutable, nil
}

// GetDefaultsFromSlices separates the values from a slice
// into 2 lists: one with elements that match the passed schema and one with the
// elements that do not match the schema.
func GetDefaultsFromSlices[K comparable, V any](
	sliceDefaults []V, mapSchema map[K]V) ([]V, []V, error) {
	editable := make([]V, 0, len(sliceDefaults))
	immutable := make([]V, 0, len(sliceDefaults))
	// Ensure that each value of the slice matches the schema.
	for _, defaultValue := range sliceDefaults {
		switch valueType := any(defaultValue).(type) {
		case map[K]V:
			schemaValueType := any(mapSchema).(map[K]V)
			newEditable, newImmutable, err := GetDefaultsFromMaps(valueType, schemaValueType)
			if err != nil {
				return nil, nil, err
			}
			if len(newEditable) != 0 {
				editable = append(editable, any(newEditable).(V))
			}
			if len(newImmutable) != 0 {
				immutable = append(immutable, any(newImmutable).(V))
			}
		default:
			editable = append(editable, defaultValue)
		}
	}
	return editable, immutable, nil
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

	if data, err := os.ReadFile(DefaultServiceCAFile); err != nil {
		return fmt.Errorf("failed to read service CA file '%s': %w", DefaultServiceCAFile, err)
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

// AddCABundle to an existing TLS configuration
func AddCABundle(config *tls.Config, caBundle string) error {
	data, err := os.ReadFile(caBundle)
	if err != nil {
		return fmt.Errorf("failed to read CA bundle '%s': %w", caBundle, err)
	}

	if config.RootCAs == nil {
		config.RootCAs = x509.NewCertPool()
	}
	config.RootCAs.AppendCertsFromPEM(data)

	return nil
}

// GetInternalClientTLSConfig creates a tls.Config that uses a dynamic loader to handle updates to the certificate and/or key.
func GetInternalClientTLSConfig(ctx context.Context) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	err := AddCABundle(tlsConfig, DefaultServiceCAFile)
	if err != nil {
		return nil, err
	}

	return tlsConfig, nil
}

// GetClientTLSConfig creates a tls.Config that uses a dynamic loader to handle updates to the certificate and/or key.
func GetClientTLSConfig(ctx context.Context, certFile, keyFile, caFile string) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if caFile != "" {
		err := AddCABundle(tlsConfig, caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to add ca bundle: %w", err)
		}
	}

	if certFile != "" {
		loader, err := dynamiccertificates.NewDynamicServingContentFromFiles("tls-client", certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to setup certificate loader: %w", err)
		}
		go loader.Run(ctx, 1)

		tlsConfig.GetClientCertificate = func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, key := loader.CurrentCertKeyContent()
			result, err := tls.X509KeyPair(cert, key)
			if err != nil {
				return nil, fmt.Errorf("failed to create client certificate: %w", err)
			}
			return &result, nil
		}
	}

	return tlsConfig, nil
}

// GetServerTLSConfig creates a tls.Config that uses a dynamic loader to handle updates to the certificate and/or key.
func GetServerTLSConfig(ctx context.Context, certFile, keyFile string) (*tls.Config, error) {
	loader, err := dynamiccertificates.NewDynamicServingContentFromFiles("tls-server", certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to setup certificate loader: %w", err)
	}
	go loader.Run(ctx, 1)

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			certBytes, keyBytes := loader.CurrentCertKeyContent()
			cert, err := tls.X509KeyPair(certBytes, keyBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to create server certificate: %w", err)
			}
			return &cert, nil
		},
	}

	return tlsConfig, nil
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

// CreateDefaultInventoryCR creates the default Inventory CR so that the system has running servers
func CreateDefaultInventoryCR(ctx context.Context, c client.Client) error {
	inventory := inventoryv1alpha1.Inventory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultInventoryCR,
			Namespace: GetEnvOrDefault(DefaultNamespaceEnvName, DefaultNamespace),
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

// GetDatabaseHostname returns the URL used to access the database service
func GetDatabaseHostname() string {
	hostname, exists := os.LookupEnv(DatabaseHostnameEnvVar)
	if !exists {
		return fmt.Sprintf("%s.%s.svc.cluster.local",
			InventoryDatabaseServerName, GetEnvOrDefault(DefaultNamespaceEnvName, DefaultNamespace))
	}
	return hostname
}

// GetPasswordOrRandom attempts to query a password from the environment and generates a random password if none was
// found matching the supplied environment variable name.
func GetPasswordOrRandom(envName string) string {
	return GetEnvOrDefault(envName, uuid.Must(uuid.NewRandom()).String())
}

// GetServiceURL constructs the default service URL for a server
func GetServiceURL(serverName string) string {
	return fmt.Sprintf("https://%s.%s.svc.cluster.local:%s", serverName, GetEnvOrDefault(DefaultNamespaceEnvName, DefaultNamespace), os.Getenv(InternalServicePortName))
}

// MakeUUIDFromNames generates a namespaced uuid value from the specified namespace and name values.  The values are
// scoped to a `cloudID` to avoid conflicts with other systems.
func MakeUUIDFromNames(namespace string, cloudID uuid.UUID, names ...string) uuid.UUID {
	value := fmt.Sprintf("%s/%s", cloudID.String(), strings.Join(names, "/"))
	namespaceUUID := uuid.MustParse(namespace)
	return uuid.NewSHA1(namespaceUUID, []byte(value))
}

// ConvertMapAnyToString converts a map of any to a map of strings.  Values not of type string are
// ignored.
func ConvertMapAnyToString(input map[string]any) map[string]string {
	output := make(map[string]string)
	for key, value := range input {
		if _, ok := input[key].(string); ok {
			output[key] = value.(string)
		}
	}
	return output
}

// GenerateSearchApiUrl appends graphql path to the backend URL to form the fully qualified search path
func GenerateSearchApiUrl(backendURL string) (string, error) {
	u, err := url.Parse(backendURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse backend URL %s: %w", backendURL, err)
	}

	// Split URL address
	hostArr := strings.Split(u.Host, ".")

	// Generate search API URL
	searchUri := strings.Join(hostArr, ".")
	return fmt.Sprintf("%s://%s/searchapi/graphql", u.Scheme, searchUri), nil
}

// Generates the name for a node's secret from a node map.
func GenerateSecretName(nodeMap map[string]interface{}, provisioningRequest string) (string, error) {
	nodeHostnameInterface, nodeHostnameExists := nodeMap["hostName"]
	if !nodeHostnameExists {
		return "", NewInputError(
			`\"hostname\" key expected to exist in `+
				`spec.templateParameters.clusterInstanceParameters `+
				`of ProvisioningRequest %s, but it's missing`,
			provisioningRequest,
		)
	}
	secretName := ExtractBeforeDot(strings.ToLower(nodeHostnameInterface.(string))) + "-bmc-secret"
	return secretName, nil
}

func DetermineAuthType(callback string) commonapi.AuthType {
	// At this time, only the OAuth and ServiceAccount authTypes are supported
	// Set authType to OAuth
	authType := commonapi.OAuth
	if strings.Contains(callback, "svc.cluster.local") {
		authType = commonapi.ServiceAccount
	}
	return authType
}

func IsValidURL(u string) bool {
	parsed, err := url.ParseRequestURI(u)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

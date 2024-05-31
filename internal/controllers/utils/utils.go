package utils

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var oranUtilsLog = ctrl.Log.WithName("oranUtilsLog")

func CreateK8sCR(ctx context.Context, c client.Client,
	newObject client.Object, ownerObject client.Object,
	operation string) (err error) {

	// Get the name and namespace of the object:
	key := client.ObjectKeyFromObject(newObject)
	oranUtilsLog.Info("[CreateK8sCR] Resource", "name", key.Name)

	// We can set the owner reference only for objects that live in the same namespace, as cross
	// namespace owners are forbidden. This also applies to non-namespaced objects like cluster
	// roles or cluster role bindings; those have empty namespaces so the equals comparison
	// should also work.
	if ownerObject.GetNamespace() == key.Namespace {
		err = controllerutil.SetControllerReference(ownerObject, newObject, c.Scheme())
		if err != nil {
			return err
		}
	}

	// Create an empty object of the same type of the new object. We will use it to fetch the
	// current state.
	objectType := reflect.TypeOf(newObject).Elem()
	oldObject := reflect.New(objectType).Interface().(client.Object)

	err = c.Get(ctx, key, oldObject)

	// If there was an error obtaining the CR and the error was "Not found", create the object.
	// If any other other occurred, return the error.
	// If the CR already exists, patch it or update it.
	if err != nil {
		if errors.IsNotFound(err) {
			oranUtilsLog.Info("[CreateK8sCR] CR not found, CREATE it")
			err = c.Create(ctx, newObject)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		newObject.SetResourceVersion(oldObject.GetResourceVersion())
		if operation == PATCH {
			oranUtilsLog.Info("[CreateK8sCR] CR already present, PATCH it")
			return c.Patch(ctx, oldObject, client.MergeFrom(newObject))
		} else if operation == UPDATE {
			oranUtilsLog.Info("[CreateK8sCR] CR already present, UPDATE it")
			return c.Update(ctx, newObject)
		}
	}

	return nil
}

func DoesK8SResourceExist(ctx context.Context, c client.Client, Name string, Namespace string, obj client.Object) (resourceExists bool, err error) {
	err = c.Get(ctx, types.NamespacedName{Name: Name, Namespace: Namespace}, obj)

	if err != nil {
		if errors.IsNotFound(err) {
			oranUtilsLog.Info("[doesK8SResourceExist] Resource not found, create it. ",
				"Type: ", reflect.TypeOf(obj), "Name: ", Name, "Namespace: ", Namespace)
			return false, nil
		} else {
			return false, err
		}
	} else {
		oranUtilsLog.Info("[doesK8SResourceExist] Resource already present, return. ",
			"Type: ", reflect.TypeOf(obj), "Name: ", Name, "Namespace: ", Namespace)
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
	// MetadataServer:
	if serverName == ORANO2IMSMetadataServerName {
		result = slices.Clone(MetadataServerArgs)
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--external-address=https://%s", orano2ims.Spec.IngressHost))

		return
	}

	// ResourceServer:
	if serverName == ORANO2IMSResourceServerName {
		searchAPI := orano2ims.Spec.ResourceServerConfig.BackendURL
		if searchAPI == "" {
			searchAPI, err = getSearchAPI(ctx, c, orano2ims)
			if err != nil {
				return nil, err
			}
		}

		result = slices.Clone(ResourceServerArgs)

		// Add the cloud-id and backend-url args:
		result = append(
			result,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", searchAPI))

		// Add the token arg:
		result = append(
			result,
			GetBackendTokenArg(orano2ims.Spec.ResourceServerConfig.BackendToken))

		return result, nil
	}

	// DeploymentManagerServer:
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
		result = append(
			result,
			fmt.Sprintf("--backend-url=%s", backendURL),
		)

		// Add the token argument:
		result = append(
			result,
			GetBackendTokenArg(orano2ims.Spec.DeploymentManagerServerConfig.BackendToken))

		// Add the extensions:
		extensionsArgsArray := extensionsToExtensionArgs(orano2ims.Spec.DeploymentManagerServerConfig.Extensions)
		result = append(result, extensionsArgsArray...)

		return
	}

	return
}

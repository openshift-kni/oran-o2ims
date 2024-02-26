package utils

import (
	"context"
	"fmt"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var oranUtilsLog = ctrl.Log.WithName("oranUtilsLog")

func CreateK8sCR(ctx context.Context, c client.Client, Name string, Namespace string,
	newObject client.Object, ownerObject client.Object, oldObject client.Object,
	runtimeScheme *runtime.Scheme, operation string) (err error) {

	oranUtilsLog.Info("[CreateK8sCR] Resource", "name", Name)
	// Set owner reference.
	if err = controllerutil.SetControllerReference(ownerObject, newObject, runtimeScheme); err != nil {
		return err
	}

	// Check if the CR already exists.
	err = c.Get(ctx, types.NamespacedName{Name: Name, Namespace: Namespace}, oldObject)

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

func BuildServerContainerArgs(orano2ims *oranv1alpha1.ORANO2IMS, serverName string) []string {
	if serverName == ORANO2IMSMetadataServerName {
		containerArgs := MetadataServerArgs
		containerArgs = append(containerArgs,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--external-address=https://%s", orano2ims.Spec.IngressHost))

		return containerArgs
	}

	if serverName == ORANO2IMSResourceServerName {
		containerArgs := ResourceServerArgs
		containerArgs = append(containerArgs,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", orano2ims.Spec.SearchAPIBackendURL),
			fmt.Sprintf("--backend-token=%s", orano2ims.Spec.BackendToken))

		return containerArgs
	}

	if serverName == ORANO2IMSDeploymentManagerServerName {
		containerArgs := DeploymentManagerServerArgs

		containerArgs = append(containerArgs,
			fmt.Sprintf("--cloud-id=%s", orano2ims.Spec.CloudId),
			fmt.Sprintf("--backend-url=%s", orano2ims.Spec.BackendURL),
			fmt.Sprintf("--backend-token=%s", orano2ims.Spec.BackendToken),
			fmt.Sprintf("--backend-type=%s", orano2ims.Spec.BackendType))

		extensionsArgsArray := extensionsToExtensionArgs(orano2ims.Spec.Extensions)
		containerArgs = append(containerArgs, extensionsArgsArray...)

		return containerArgs
	}

	return nil
}

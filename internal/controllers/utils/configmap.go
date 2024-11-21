package utils

import (
	"context"
	"embed"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateConfigMapFromEmbeddedFile extracts a file from an embedded file system and builds a ConfigMap.  If the file
// does not exist or is not accessible then an error is returned.
func CreateConfigMapFromEmbeddedFile(fs embed.FS, path, namespace, name, key string) (*corev1.ConfigMap, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded file %s: %w", path, err)
	}

	configmap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string]string{
			key: string(data),
		},
	}

	return configmap, nil
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

/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateSecretFromLiterals takes a map of key value pairs and produces a Secret.
func CreateSecretFromLiterals(ctx context.Context, c client.Client, ownerObject client.Object, namespace, name string, literals map[string][]byte) error {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: literals,
	}

	err := CreateK8sCR(ctx, c, secret, ownerObject, UPDATE)
	if err != nil {
		return fmt.Errorf("failed to create secret %s/%s: %w", namespace, name, err)
	}

	return nil
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

// CopyK8sSecret copies a secret from one namespace to another.
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
	// Remove any existing owner references
	secret.ObjectMeta.OwnerReferences = []metav1.OwnerReference{}

	// Create the secret in the target namespace
	err = CreateK8sCR(ctx, c, secret, nil, UPDATE)
	if err != nil {
		return fmt.Errorf("failed to create secret %s in namespace %s: %w", secret.GetName(), secret.GetNamespace(), err)
	}
	return nil
}

// GetKeyPairFromSecret retrieves a certificate and its associated private key from a Secret.
func GetKeyPairFromSecret(ctx context.Context, c client.Client, name, namespace string) ([]byte, []byte, error) {
	secret, err := GetSecret(ctx, c, name, namespace)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve secret '%s': %w", name, err)
	}

	certBytes, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, nil, NewInputError("secret '%s' does not contain key 'tls.crt'", name)
	}

	keyBytes, ok := secret.Data["tls.key"]
	if !ok {
		return nil, nil, NewInputError("secret '%s' does not contain key 'tls.key'", name)
	}

	return certBytes, keyBytes, nil
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/constants"
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

// GetKeyPairFromSecret retrieves a certificate and its associated private key from a Secret.
func GetKeyPairFromSecret(ctx context.Context, c client.Client, name, namespace string) ([]byte, []byte, error) {
	secret, err := GetSecret(ctx, c, name, namespace)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve secret '%s': %w", name, err)
	}

	certBytes, ok := secret.Data[constants.TLSCertField]
	if !ok {
		return nil, nil, NewInputError("secret '%s' does not contain key 'tls.crt'", name)
	}

	keyBytes, ok := secret.Data[constants.TLSKeyField]
	if !ok {
		return nil, nil, NewInputError("secret '%s' does not contain key 'tls.key'", name)
	}

	return certBytes, keyBytes, nil
}

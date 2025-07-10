/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package k8s

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/api/errors"

	agentv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	pluginsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/plugins/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
)

const (
	// https://gist.github.com/dlbewley/f57eb2bb5b69d2db0df7b171329a68cc
	secretTypeLabel      = "hive.openshift.io/secret-type"
	secretTypeLabelValue = "kubeconfig"
)

// NewClientForHub creates a new client for the hub cluster
func NewClientForHub() (client.WithWatch, error) {
	conf, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	c, err := client.NewWithWatch(conf, client.Options{Scheme: GetSchemeForHub()})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return c, nil
}

// GetSchemeForHub returns the scheme for the hub cluster client
func GetSchemeForHub() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta2.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(agentv1beta1.AddToScheme(scheme))
	utilruntime.Must(provisioningv1alpha1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(hwmgmtv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pluginsv1alpha1.AddToScheme(scheme))

	return scheme
}

// NewClientForCluster creates a new client for a managed cluster
func NewClientForCluster(ctx context.Context, hubClient client.Client, clusterName string) (client.Client, error) {
	kubeConfig, err := GetClusterKubeConfigFromSecret(ctx, hubClient, clusterName)
	if err != nil {
		return nil, err
	}

	conf, err := clientcmd.RESTConfigFromKubeConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	c, err := client.New(conf, client.Options{Scheme: GetSchemeForCluster()})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return c, nil
}

// GetClusterKubeConfigFromSecret retrieves the cluster kubeconfig from a secret
func GetClusterKubeConfigFromSecret(ctx context.Context, hubClient client.Client, clusterName string) ([]byte, error) {
	selector := labels.NewSelector()
	kubeConfigSelector, _ := labels.NewRequirement(secretTypeLabel, selection.Equals, []string{secretTypeLabelValue})

	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	var secrets corev1.SecretList
	err := hubClient.List(ctxWithTimeout, &secrets, &client.ListOptions{
		Namespace:     clusterName,
		LabelSelector: selector.Add(*kubeConfigSelector),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	if len(secrets.Items) == 0 {
		return nil, fmt.Errorf("no kubeconfig secret found for managed cluster %s", clusterName)
	}

	return secrets.Items[0].Data["kubeconfig"], nil
}

// GetSchemeForCluster returns the scheme for the managed cluster client
func GetSchemeForCluster() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))

	return scheme
}

// CreateOrUpdate attempts to update an existing Kubernetes object, or creates it if it doesn't exist.
// This implements an "upsert" operation for Kubernetes resources.
func CreateOrUpdate(ctx context.Context, c client.Client, obj client.Object) error {
	// Try to get existing object
	existing := obj.DeepCopyObject().(client.Object)
	key := client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}

	if err := c.Get(ctx, key, existing); err != nil {
		if errors.IsNotFound(err) { // Create if not found, otherwise return error
			slog.Info("Creating a new resource", "gvk", obj.GetObjectKind().GroupVersionKind().String(),
				"namespace", obj.GetNamespace(), "name", obj.GetName())
			return c.Create(ctx, obj) //nolint:wrapcheck
		}
		return fmt.Errorf("failed to get existing object: %w", err)
	}

	// Update existing object
	obj.SetResourceVersion(existing.GetResourceVersion())
	slog.Info("Updating an existing resource", "gvk", obj.GetObjectKind().GroupVersionKind().String(),
		"namespace", obj.GetNamespace(), "name", obj.GetName())
	return c.Update(ctx, obj) //nolint:wrapcheck
}

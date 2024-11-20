package k8s_client

import (
	"context"
	"fmt"
	"time"

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

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

const (
	ListRequestTimeout   = 30 * time.Second
	SingleRequestTimeout = 10 * time.Second

	// https://gist.github.com/dlbewley/f57eb2bb5b69d2db0df7b171329a68cc
	secretTypeLabel      = "hive.openshift.io/secret-type"
	secretTypeLabelValue = "kubeconfig"
)

// NewClientForHub creates a new client for the hub cluster
func NewClientForHub() (client.Client, error) {
	conf, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	c, err := client.New(conf, client.Options{Scheme: GetSchemeForHub()})
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

	return scheme
}

// NewClientForCluster creates a new client for a managed cluster
func NewClientForCluster(ctx context.Context, hubClient client.Client, clusterName string) (client.Client, error) {
	kubeConfig, err := getClusterKubeConfigFromSecret(ctx, hubClient, clusterName)
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

// getClusterKubeConfigFromSecret retrieves the cluster kubeconfig from a secret
func getClusterKubeConfigFromSecret(ctx context.Context, hubClient client.Client, clusterName string) ([]byte, error) {
	selector := labels.NewSelector()
	kubeConfigSelector, _ := labels.NewRequirement(secretTypeLabel, selection.Equals, []string{secretTypeLabelValue})

	ctxWithTimeout, cancel := context.WithTimeout(ctx, ListRequestTimeout)
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

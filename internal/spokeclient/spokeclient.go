/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package spokeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	managedServiceAccountAddonName = "managed-serviceaccount"
	defaultAddonInstallNamespace   = "open-cluster-management-agent-addon"
)

// Clients holds the spoke clients built from a scoped MSA token.
type Clients struct {
	Client    client.Client
	Clientset kubernetes.Interface
}

const (
	// RuntimeClientOnly skips building a kubernetes.Interface.
	RuntimeClientOnly = false
	// WithClientset also builds a kubernetes.Interface from the scoped token.
	WithClientset = true
)

type spokeClientEntry struct {
	clients              Clients
	tokenResourceVersion string
}

var (
	spokeClientsMu sync.RWMutex
	spokeClients   = make(map[string]*spokeClientEntry)
)

// newSpokeClientFunc builds a controller-runtime spoke client from connection details.
// Extracted as a package-level variable so tests can override it.
var newSpokeClientFunc = buildSpokeClient

// newSpokeClientsetFunc builds a kubernetes clientset from the same connection details.
var newSpokeClientsetFunc = buildSpokeClientset

// SetTestSpokeClientCreator overrides the spoke client builder for tests.
// It returns a restore function that resets the original builder.
func SetTestSpokeClientCreator(fn func(apiServerURL, token string, caCert []byte, spokeScheme *runtime.Scheme) (client.Client, error)) func() {
	orig := newSpokeClientFunc
	newSpokeClientFunc = fn
	return func() { newSpokeClientFunc = orig }
}

// SetTestSpokeClientsetCreator overrides the spoke clientset builder for tests.
// It returns a restore function that resets the original builder.
func SetTestSpokeClientsetCreator(fn func(apiServerURL, token string, caCert []byte) (kubernetes.Interface, error)) func() {
	orig := newSpokeClientsetFunc
	newSpokeClientsetFunc = fn
	return func() { newSpokeClientsetFunc = orig }
}

// NewSpokeScheme creates a scheme from the provided installer functions.
func NewSpokeScheme(installers ...func(*runtime.Scheme) error) *runtime.Scheme {
	s := runtime.NewScheme()
	for _, fn := range installers {
		utilruntime.Must(fn(s))
	}
	return s
}

func spokeRESTConfig(apiServerURL, token string, caCert []byte) *rest.Config {
	tlsConfig := rest.TLSClientConfig{}
	if len(caCert) > 0 {
		tlsConfig.CAData = caCert
	}
	return &rest.Config{
		Host:            apiServerURL,
		BearerToken:     token,
		TLSClientConfig: tlsConfig,
	}
}

// buildSpokeClient creates a controller-runtime client for a spoke cluster
// using bearer token authentication and the provided scheme.
func buildSpokeClient(apiServerURL, token string, caCert []byte, spokeScheme *runtime.Scheme) (client.Client, error) {
	config := spokeRESTConfig(apiServerURL, token, caCert)
	c, err := client.New(config, client.Options{Scheme: spokeScheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create spoke client: %w", err)
	}
	return c, nil
}

// buildSpokeClientset creates a kubernetes clientset for a spoke cluster
// using bearer token authentication.
func buildSpokeClientset(apiServerURL, token string, caCert []byte) (kubernetes.Interface, error) {
	config := spokeRESTConfig(apiServerURL, token, caCert)
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create spoke clientset: %w", err)
	}
	return cs, nil
}

// EnsureSpokeClient returns a scoped spoke client, driving the MSA +
// ManifestWork lifecycle as needed. Every call reconciles the RBAC
// ManifestWork and waits for its current generation to be applied and
// available on the spoke. Cached clients are reused while the MSA token is
// unchanged and rebuilt when it rotates.
//
// Returns:
//   - (clients, true, nil): spoke client is ready to use
//   - (zero, false, nil): spoke client is not ready yet — caller should requeue
//   - (zero, false, err): real failure (API error or InputError) — caller should handle
//
// Parameters:
//   - msaName: name for the ManagedServiceAccount CR (e.g. "<pr-name>-upgrade")
//   - mwName: name for the RBAC ManifestWork (e.g. "<pr-name>-upgrade-rbac")
//   - rules: RBAC PolicyRules to deliver to the spoke via ManifestWork
//   - spokeScheme: scheme for the spoke client (determines which types it can work with)
//   - buildClientset: whether to build a kubernetes.Interface from the same token
func EnsureSpokeClient(
	ctx context.Context,
	hubClient client.Client,
	logger *slog.Logger,
	clusterName, msaName, mwName string,
	rules []rbacv1.PolicyRule,
	spokeScheme *runtime.Scheme,
	buildClientset bool,
) (Clients, bool, error) {
	// 1. Check that the managed-serviceaccount addon is available.
	addon := &addonv1alpha1.ManagedClusterAddOn{}
	if err := hubClient.Get(ctx, types.NamespacedName{
		Name: managedServiceAccountAddonName, Namespace: clusterName,
	}, addon); err != nil {
		if errors.IsNotFound(err) {
			return Clients{}, false, typederrors.NewInputError(
				"the managed-serviceaccount addon is not available on cluster %s", clusterName)
		}
		return Clients{}, false, fmt.Errorf("failed to check managed-serviceaccount addon: %w", err)
	}

	// 2. Create ManagedServiceAccount if not present.
	msa := &msav1beta1.ManagedServiceAccount{}
	if err := hubClient.Get(ctx, types.NamespacedName{
		Name: msaName, Namespace: clusterName,
	}, msa); err != nil {
		if !errors.IsNotFound(err) {
			return Clients{}, false, fmt.Errorf("failed to get ManagedServiceAccount: %w", err)
		}
		msa = &msav1beta1.ManagedServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      msaName,
				Namespace: clusterName,
			},
			Spec: msav1beta1.ManagedServiceAccountSpec{
				Rotation: msav1beta1.ManagedServiceAccountRotation{
					Enabled:  true,
					Validity: metav1.Duration{Duration: 24 * time.Hour},
				},
			},
		}
		if err := hubClient.Create(ctx, msa); err != nil {
			return Clients{}, false, fmt.Errorf("failed to create ManagedServiceAccount: %w", err)
		}
	}

	// 3. Wait for token sync — not an error, just not ready yet.
	if msa.Status.TokenSecretRef == nil || msa.Status.TokenSecretRef.Name == "" {
		evictCachedSpokeClient(msaName)
		logger.InfoContext(ctx, "Waiting for ManagedServiceAccount token to be synced",
			slog.String("clusterName", clusterName), slog.String("msaName", msaName))
		return Clients{}, false, nil
	}

	// 4. Read token Secret.
	tokenSecret := &corev1.Secret{}
	if err := hubClient.Get(ctx, types.NamespacedName{
		Name: msa.Status.TokenSecretRef.Name, Namespace: clusterName,
	}, tokenSecret); err != nil {
		if errors.IsNotFound(err) {
			// The token the cached client holds is gone; rebuild once it is reissued.
			evictCachedSpokeClient(msaName)
		}
		return Clients{}, false, fmt.Errorf("failed to read token secret: %w", err)
	}

	// 5. Ensure the RBAC ManifestWork is up to date and ready on the spoke.
	saNamespace := addon.Status.Namespace
	if saNamespace == "" {
		saNamespace = defaultAddonInstallNamespace
	}
	ready, err := ensureRBACManifestWork(ctx, hubClient, logger, clusterName, msaName, mwName, saNamespace, rules)
	if err != nil || !ready {
		return Clients{}, false, err
	}

	// 6. Reuse the cached clients while they are backed by the current token.
	spokeClientsMu.RLock()
	entry, cached := spokeClients[msaName]
	spokeClientsMu.RUnlock()
	if cached && entry.tokenResourceVersion == tokenSecret.ResourceVersion &&
		(!buildClientset || entry.clients.Clientset != nil) {
		return entry.clients, true, nil
	}
	if cached && entry.tokenResourceVersion != tokenSecret.ResourceVersion {
		logger.InfoContext(ctx, "Spoke client token rotated, rebuilding client",
			slog.String("clusterName", clusterName))
	}

	// 7. Build spoke clients from the scoped token.
	token, caCert, err := extractTokenData(tokenSecret, clusterName)
	if err != nil {
		return Clients{}, false, err
	}
	apiServerURL, err := getAPIServerURL(ctx, hubClient, clusterName)
	if err != nil {
		return Clients{}, false, err
	}
	c, err := newSpokeClientFunc(apiServerURL, string(token), caCert, spokeScheme)
	if err != nil {
		return Clients{}, false, err
	}
	clients := Clients{Client: c}
	if buildClientset {
		cs, err := newSpokeClientsetFunc(apiServerURL, string(token), caCert)
		if err != nil {
			return Clients{}, false, err
		}
		clients.Clientset = cs
	}

	// 8. Cache the clients.
	spokeClientsMu.Lock()
	spokeClients[msaName] = &spokeClientEntry{
		clients:              clients,
		tokenResourceVersion: tokenSecret.ResourceVersion,
	}
	spokeClientsMu.Unlock()

	return clients, true, nil
}

// evictCachedSpokeClient drops any cached client for the MSA so that the next
// successful setup rebuilds it from a freshly issued token. Call it when the
// token backing the cached client is gone.
func evictCachedSpokeClient(msaName string) {
	spokeClientsMu.Lock()
	delete(spokeClients, msaName)
	spokeClientsMu.Unlock()
}

// ensureRBACManifestWork creates or patches the ManifestWork to match the desired
// RBAC rules and service account binding. It returns true only when the current
// generation is applied and its resources are available on the spoke.
func ensureRBACManifestWork(
	ctx context.Context, hubClient client.Client, logger *slog.Logger,
	clusterName, msaName, mwName, saNamespace string, rules []rbacv1.PolicyRule,
) (bool, error) {
	desired, err := BuildRBACManifestWork(mwName, clusterName, msaName, saNamespace, rules)
	if err != nil {
		return false, fmt.Errorf("failed to build RBAC ManifestWork: %w", err)
	}
	// A ManifestWork that is being deleted needs both guards below, and they do
	// different jobs: leaving the workload untouched in the mutate function
	// keeps CreateOrPatch from issuing a write, and the check after it reports
	// not-ready so the caller waits for the delete to finish and the work to be
	// recreated on a later call.
	mw := &workv1.ManifestWork{ObjectMeta: metav1.ObjectMeta{Name: mwName, Namespace: clusterName}}
	result, err := controllerutil.CreateOrPatch(ctx, hubClient, mw, func() error {
		if !mw.DeletionTimestamp.IsZero() {
			return nil
		}
		mw.Spec.Workload = desired.Spec.Workload
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("failed to reconcile RBAC ManifestWork: %w", err)
	}
	if !mw.DeletionTimestamp.IsZero() {
		return false, nil
	}
	if result != controllerutil.OperationResultNone {
		logger.InfoContext(ctx, "Reconciled RBAC ManifestWork",
			slog.String("clusterName", clusterName), slog.String("mwName", mwName), slog.String("operation", string(result)))
	}

	// Readiness is decided solely by the conditions observing the generation we
	// just wrote. A create leaves the status empty and a spec change bumps the
	// generation past what the conditions observed, so both report not-ready
	// here without needing to be special-cased on the write result.
	//
	// This relies on the OCM work agent stamping observedGeneration on the
	// Applied and Available conditions with the ManifestWork's generation. It
	// has done so since at least ACM 2.14 and the prerequisites require ACM
	// 2.15 or newer, so the supported floor is covered. An agent that left
	// observedGeneration unset would never match and spoke access would stay
	// not-ready for good, so confirm the behavior still holds when moving to a
	// new ACM release.
	for _, conditionType := range []string{workv1.WorkApplied, workv1.WorkAvailable} {
		condition := apimeta.FindStatusCondition(mw.Status.Conditions, conditionType)
		if condition == nil || condition.Status != metav1.ConditionTrue || condition.ObservedGeneration != mw.Generation {
			logger.InfoContext(ctx, "Waiting for current RBAC ManifestWork to be applied and available on spoke",
				slog.String("clusterName", clusterName), slog.String("mwName", mwName))
			return false, nil
		}
	}
	return true, nil
}

// CleanupSpokeAccess deletes the ManagedServiceAccount and RBAC ManifestWork,
// and clears the cached spoke client.
func CleanupSpokeAccess(
	ctx context.Context,
	hubClient client.Client,
	clusterName, msaName, mwName string,
) error {
	msa := &msav1beta1.ManagedServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: msaName, Namespace: clusterName},
	}
	if err := client.IgnoreNotFound(hubClient.Delete(ctx, msa)); err != nil {
		return fmt.Errorf("failed to delete ManagedServiceAccount: %w", err)
	}

	mw := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{Name: mwName, Namespace: clusterName},
	}
	if err := client.IgnoreNotFound(hubClient.Delete(ctx, mw)); err != nil {
		return fmt.Errorf("failed to delete RBAC ManifestWork: %w", err)
	}

	evictCachedSpokeClient(msaName)

	return nil
}

// extractTokenData validates and extracts the token and CA certificate from
// the MSA token Secret. Returns an InputError if required keys are missing.
func extractTokenData(secret *corev1.Secret, clusterName string) (token, caCert []byte, err error) {
	token, ok := secret.Data["token"]
	if !ok || len(token) == 0 {
		return nil, nil, typederrors.NewInputError(
			"token secret %s/%s is missing the 'token' key", clusterName, secret.Name)
	}
	caCert, ok = secret.Data["ca.crt"]
	if !ok || len(caCert) == 0 {
		return nil, nil, typederrors.NewInputError(
			"token secret %s/%s is missing the 'ca.crt' key", clusterName, secret.Name)
	}
	return token, caCert, nil
}

// getAPIServerURL reads the API server URL from the ManagedCluster resource.
func getAPIServerURL(
	ctx context.Context,
	hubClient client.Client,
	clusterName string,
) (string, error) {
	mc := &clusterv1.ManagedCluster{}
	if err := hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, mc); err != nil {
		return "", fmt.Errorf("failed to get ManagedCluster: %w", err)
	}
	if len(mc.Spec.ManagedClusterClientConfigs) == 0 {
		return "", fmt.Errorf("managedCluster %s has no client configs", clusterName)
	}
	return mc.Spec.ManagedClusterClientConfigs[0].URL, nil
}

// BuildRBACManifestWork constructs a ManifestWork that delivers a ClusterRole
// and ClusterRoleBinding to the spoke cluster.
func BuildRBACManifestWork(mwName, clusterName, saName, saNamespace string, rules []rbacv1.PolicyRule) (*workv1.ManifestWork, error) {
	clusterRole := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: mwName + "-role",
		},
		Rules: rules,
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: mwName + "-binding",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     mwName + "-role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: saNamespace,
			},
		},
	}

	crBytes, err := json.Marshal(clusterRole)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ClusterRole: %w", err)
	}
	crbBytes, err := json.Marshal(clusterRoleBinding)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ClusterRoleBinding: %w", err)
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwName,
			Namespace: clusterName,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: crBytes}},
					{RawExtension: runtime.RawExtension{Raw: crbBytes}},
				},
			},
		},
	}, nil
}

// ClearCache removes all cached spoke clients. Intended for test cleanup.
func ClearCache() {
	spokeClientsMu.Lock()
	spokeClients = make(map[string]*spokeClientEntry)
	spokeClientsMu.Unlock()
}

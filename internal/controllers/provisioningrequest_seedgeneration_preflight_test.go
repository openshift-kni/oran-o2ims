/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGetSeedGenerationAdminKubeconfigUsesClusterDeploymentReference(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "spoke-a"}
	kubeconfig := makeSeedGenerationAdminKubeconfig(t)
	clusterDeployment := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", Namespace: "spoke-a"},
		Spec: hivev1.ClusterDeploymentSpec{ClusterMetadata: &hivev1.ClusterMetadata{
			AdminKubeconfigSecretRef: corev1.LocalObjectReference{Name: "admin-kubeconfig"},
		}},
	}
	adminSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "admin-kubeconfig", Namespace: "spoke-a"},
		Data:       map[string][]byte{"kubeconfig": kubeconfig},
	}
	decoySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "other-kubeconfig", Namespace: "spoke-a"},
		Data:       map[string][]byte{"kubeconfig": []byte("not the admin kubeconfig")},
	}
	task := seedTestTask(pr, newSeedTestClient(t, clusterDeployment, adminSecret, decoySecret))
	got, err := task.getSeedGenerationAdminKubeconfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, kubeconfig) {
		t.Fatal("did not use the kubeconfig named by ClusterDeployment.spec.clusterMetadata.adminKubeconfigSecretRef")
	}
}

func TestGetSeedGenerationAdminKubeconfigRejectsBearerToken(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "spoke-a"}
	clusterDeployment := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "spoke-a", Namespace: "spoke-a"},
		Spec: hivev1.ClusterDeploymentSpec{ClusterMetadata: &hivev1.ClusterMetadata{
			AdminKubeconfigSecretRef: corev1.LocalObjectReference{Name: "admin-kubeconfig"},
		}},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "admin-kubeconfig", Namespace: "spoke-a"},
		Data:       map[string][]byte{"kubeconfig": []byte(`{"apiVersion":"v1","kind":"Config","clusters":[{"name":"spoke","cluster":{"server":"https://spoke.example"}}],"contexts":[{"name":"spoke","context":{"cluster":"spoke","user":"admin"}}],"current-context":"spoke","users":[{"name":"admin","user":{"token":"service-account-token"}}]}`)},
	}
	task := seedTestTask(pr, newSeedTestClient(t, clusterDeployment, secret))
	if _, err := task.getSeedGenerationAdminKubeconfig(ctx); err == nil || !strings.Contains(err.Error(), "client certificate") {
		t.Fatalf("bearer token kubeconfig was not rejected: %v", err)
	}
}

func makeSeedGenerationAdminKubeconfig(t *testing.T) []byte {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "system:admin"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	config := map[string]any{
		"apiVersion": "v1",
		"kind":       "Config",
		"clusters": []any{map[string]any{"name": "spoke", "cluster": map[string]any{
			"server": "https://spoke.example", "certificate-authority-data": "Y2E=",
		}}},
		"contexts":        []any{map[string]any{"name": "spoke", "context": map[string]any{"cluster": "spoke", "user": "admin"}}},
		"current-context": "spoke",
		"users": []any{map[string]any{"name": "admin", "user": map[string]any{
			"client-certificate-data": base64.StdEncoding.EncodeToString(certificatePEM),
			"client-key-data":         base64.StdEncoding.EncodeToString(privateKeyPEM),
		}}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSeedGenerationTransientErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "API conflict",
			err: apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "snapshot",
				errors.New("resource version changed")),
			want: true,
		},
		{name: "API unavailable", err: apierrors.NewServiceUnavailable("try again"), want: true},
		{name: "connection refused", err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, want: true},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "registry server error", err: seedGenerationHTTPError(http.StatusServiceUnavailable, "registry unavailable"), want: true},
		{name: "invalid input", err: errors.New("invalid seed generation configuration"), want: false},
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "pull-secret", errors.New("denied")), want: false},
		{name: "registry unauthorized", err: seedGenerationHTTPError(http.StatusUnauthorized, "registry unauthorized"), want: false},
		{name: "DNS lookup failure retries", err: &net.DNSError{Err: "no such host", Name: "invalid.example", IsNotFound: true}, want: true},
		{name: "HTTP wrapped DNS failure retries", err: &url.Error{Op: "Post", Err: &net.DNSError{IsNotFound: true}}, want: true},
		{name: "HTTP wrapped invalid CA fails", err: &url.Error{Op: "Post", Err: x509.UnknownAuthorityError{}}, want: false},
		{name: "HTTP wrapped malformed request fails", err: &url.Error{Op: "Post", Err: errors.New("invalid request")}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSeedGenerationTransientError(tc.err); got != tc.want {
				t.Fatalf("isSeedGenerationTransientError(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestHandleSeedGenerationPreflightError(t *testing.T) {
	ctx := context.Background()
	transientPR := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "transient-pr"}}
	transientTask := seedTestTask(transientPR, newSeedTestClient(t, transientPR.DeepCopy()))
	_, err := transientTask.handleSeedGenerationPreflightError(ctx, "preflight failed", apierrors.NewConflict(
		schema.GroupResource{Resource: "configmaps"}, "snapshot", errors.New("resource version changed")))
	if err == nil {
		t.Fatal("transient preflight error did not request retry")
	}
	if condition := seedGenerationCondition(transientPR); condition != nil {
		t.Fatalf("transient error was recorded as terminal: %+v", condition)
	}

	invalidPR := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Name: "invalid-pr"}}
	invalidClient := newSeedTestClient(t, invalidPR.DeepCopy())
	if err := invalidClient.Get(ctx, client.ObjectKeyFromObject(invalidPR), invalidPR); err != nil {
		t.Fatalf("failed to get invalid test ProvisioningRequest: %v", err)
	}
	invalidTask := seedTestTask(invalidPR, invalidClient)
	_, err = invalidTask.handleSeedGenerationPreflightError(ctx, "preflight failed", errors.New("invalid input"))
	if err != nil {
		t.Fatalf("terminal input error failed to persist status: %v", err)
	}
	if condition := seedGenerationCondition(invalidPR); condition == nil ||
		condition.Reason != string(provisioningv1alpha1.CRconditionReasons.PreconditionChecksFailed) {
		t.Fatalf("invalid input was not recorded as a terminal precondition failure: %+v", condition)
	}
}

func TestMakeSeedGenerationSnapshotObjects(t *testing.T) {
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "11111111-1111-4111-8111-111111111111", UID: types.UID("pr-uid"),
	}}
	document := &seedGenerationInputDocument{
		ClusterTemplateName:      "seed-template.v1",
		ClusterTemplateNamespace: "templates",
		Release:                  "4.22.0",
		SeedGeneration:           map[string]any{"seedImage": "quay.io/example/seed:4.22"},
	}
	secretValue := []byte(`{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`)
	configMap, secrets, err := makeSeedGenerationSnapshotObjects(pr, document, map[string]map[string][]byte{
		"seedAuth":        {seedGenerationAuthSecretKey: secretValue},
		"spokeKubeconfig": {"kubeconfig": []byte("kubeconfig")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if configMap.Name != "11111111-1111-4111-8111-111111111111-seedgen-inputs" {
		t.Fatalf("unexpected ConfigMap name %q", configMap.Name)
	}
	if configMap.Immutable == nil || !*configMap.Immutable {
		t.Fatal("input ConfigMap is not immutable")
	}
	if strings.Contains(configMap.Data[seedGenerationSnapshotConfig], string(secretValue)) {
		t.Fatal("credential bytes were copied into the input ConfigMap")
	}
	secret := secrets["seedAuth"]
	if secret == nil || secret.Immutable == nil || !*secret.Immutable {
		t.Fatal("seed auth snapshot is missing or mutable")
	}
	if !bytes.Equal(secret.Data[seedGenerationAuthSecretKey], secretValue) {
		t.Fatal("seed auth snapshot does not preserve the source bytes")
	}
	if secret.Labels[provisioningv1alpha1.ProvisioningRequestUIDLabel] != string(pr.UID) ||
		!hasSeedGenerationSnapshotOwner(secret, pr) {
		t.Fatal("seed auth snapshot is missing exact ProvisioningRequest ownership")
	}
}

func TestLoadSeedGenerationInputSnapshotAdoptsAndPinsUIDs(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("pr-uid"),
	}}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{
		SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{},
	}
	document := &seedGenerationInputDocument{
		ClusterTemplateName:      "seed-template.v1",
		ClusterTemplateNamespace: "templates",
		Release:                  "4.22.0",
		SeedGeneration:           map[string]any{"seedImage": "quay.io/example/seed:4.22"},
	}
	configMap, secrets, err := makeSeedGenerationSnapshotObjects(pr, document, map[string]map[string][]byte{
		"seedAuth":        {seedGenerationAuthSecretKey: []byte(`{"auths":{}}`)},
		"spokeKubeconfig": {"kubeconfig": []byte("kubeconfig")},
	})
	if err != nil {
		t.Fatal(err)
	}
	objects := []client.Object{configMap}
	for _, secret := range secrets {
		objects = append(objects, secret)
	}
	c := newSeedTestClient(t, append([]client.Object{pr}, objects...)...)
	task := seedTestTask(pr, c)
	snapshot, err := task.loadSeedGenerationInputSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.Document.ClusterTemplateName != "seed-template.v1" || len(snapshot.Secrets) != 2 {
		t.Fatalf("complete snapshot was not adopted: %#v", snapshot)
	}
	actualUIDs := seedGenerationInputUIDs(snapshot)
	if len(actualUIDs) != 3 {
		t.Fatalf("got %d snapshot UIDs, want ConfigMap and two Secrets", len(actualUIDs))
	}
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.InputSnapshotResourceUIDs = actualUIDs
	if _, err := task.loadSeedGenerationInputSnapshot(ctx); err != nil {
		t.Fatalf("snapshot with matching UIDs was rejected: %v", err)
	}
	for name := range actualUIDs {
		actualUIDs[name] = "replacement-uid"
		break
	}
	if _, err := task.loadSeedGenerationInputSnapshot(ctx); err == nil || !strings.Contains(err.Error(), "UID does not match") || isSeedGenerationTransientError(err) {
		t.Fatalf("snapshot replacement was not rejected: %v", err)
	}
}

func TestLoadSeedGenerationInputSnapshotRemovesIncompleteOwnedCopies(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", UID: types.UID("pr-uid"),
	}}
	document := &seedGenerationInputDocument{
		ClusterTemplateName:      "seed-template.v1",
		ClusterTemplateNamespace: "templates",
		Release:                  "4.22.0",
		SeedGeneration:           map[string]any{"seedImage": "quay.io/example/seed:4.22"},
	}
	configMap, _, err := makeSeedGenerationSnapshotObjects(pr, document, map[string]map[string][]byte{
		"seedAuth":        {seedGenerationAuthSecretKey: []byte(`{"auths":{}}`)},
		"spokeKubeconfig": {"kubeconfig": []byte("kubeconfig")},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := newSeedTestClient(t, pr, configMap)
	task := seedTestTask(pr, c)
	snapshot, err := task.loadSeedGenerationInputSnapshot(ctx)
	if snapshot != nil || !errors.Is(err, errIncompleteSeedGenerationSnapshot) {
		t.Fatalf("got snapshot %v, error %v; want incomplete snapshot cleanup", snapshot, err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(configMap), &corev1.ConfigMap{}); err == nil {
		t.Fatal("owned partial ConfigMap was not deleted")
	}
}

func TestDockerAuthRegistryMatchingAndMerge(t *testing.T) {
	seedAuth := []byte(`{"auths":{"https://index.docker.io/v1/":{"username":"seed","password":"token"}}}`)
	if err := validateDockerAuthForImage(seedAuth, "busybox:latest"); err != nil {
		t.Fatalf("Docker Hub alias did not match: %v", err)
	}
	if err := validateDockerAuthForImage(seedAuth, "quay.io/team/seed:latest"); err == nil {
		t.Fatal("auth for another registry was accepted")
	}

	scopedAuth := []byte(`{"auths":{"quay.io/redhat_emp1":{"username":"scoped","password":"secret"}}}`)
	if err := validateDockerAuthForImage(scopedAuth, "quay.io/redhat_emp1/seed:latest"); err != nil {
		t.Fatalf("repository-scoped auth was rejected: %v", err)
	}
	if err := validateDockerAuthForImage(scopedAuth, "quay.io/redhat_employee/seed:latest"); err == nil {
		t.Fatal("repository auth scope matched a different path prefix")
	}
	repositoryScopedAuth := []byte(`{"auths":{"quay.io/redhat_emp1/seedimages":{"username":"repo-scoped","password":"secret"}}}`)
	for _, image := range []string{
		"quay.io/redhat_emp1/seedimages:4.22",
		"quay.io/redhat_emp1/seedimages/variant:4.22",
	} {
		if err := validateDockerAuthForImage(repositoryScopedAuth, image); err != nil {
			t.Errorf("repository auth for server/name/repo did not match image %q: %v", image, err)
		}
	}
	if err := validateDockerAuthForImage(repositoryScopedAuth, "quay.io/redhat_emp1/seedimages-other:4.22"); err == nil {
		t.Fatal("repository auth scope matched a different repository prefix")
	}

	base := []byte(`{"auths":{"registry.redhat.io":{"username":"hub","password":"secret"}}}`)
	merged, err := mergeDockerAuthFiles(base, seedAuth)
	if err != nil {
		t.Fatal(err)
	}
	var auth dockerAuthFile
	if err := json.Unmarshal(merged, &auth); err != nil {
		t.Fatal(err)
	}
	if len(auth.Auths) != 2 {
		t.Fatalf("merged auth file has %d entries, want 2", len(auth.Auths))
	}
}

func TestSeedGenerationRegistryPreflightCachesSuccess(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
		Name: "seed-pr", Generation: 3,
	}}
	seedAuth := []byte(`{"auths":{"quay.io/redhat_emp1":{"username":"seed","password":"secret"}}}`)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "seed-auth", Namespace: "templates"},
		Data:       map[string][]byte{seedGenerationAuthSecretKey: seedAuth},
	}
	clusterTemplate := &provisioningv1alpha1.ClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "seed-template.v1", Namespace: "templates"},
	}
	seedGeneration := map[string]any{
		"seedImage":         "quay.io/redhat_emp1/seed:4.22",
		"seedAuthSecretRef": map[string]any{"name": "seed-auth"},
	}
	task := seedTestTask(pr, newSeedTestClient(t, secret))
	pushChecks := 0
	task.seedGenerationRegistryPushAccess = func(_ context.Context, image string, auth []byte) error {
		pushChecks++
		if image != "quay.io/redhat_emp1/seed:4.22" || !bytes.Equal(auth, seedAuth) {
			t.Fatalf("unexpected early registry preflight inputs: image=%q auth=%q", image, auth)
		}
		return nil
	}

	if err := task.validateSeedGenerationRegistryPreflight(ctx, clusterTemplate, seedGeneration); err != nil {
		t.Fatal(err)
	}
	if pushChecks != 1 {
		t.Fatalf("got %d push checks, want 1", pushChecks)
	}
	condition := meta.FindStatusCondition(pr.Status.Conditions,
		string(provisioningv1alpha1.PRconditionTypes.SeedGenerationRegistryValidated))
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.ObservedGeneration != pr.Generation {
		t.Fatalf("successful preflight condition is incorrect: %+v", condition)
	}

	if err := task.validateSeedGenerationRegistryPreflight(ctx, clusterTemplate, seedGeneration); err != nil {
		t.Fatal(err)
	}
	if pushChecks != 1 {
		t.Fatalf("same-generation retry repeated the registry push check %d times", pushChecks)
	}
	pr.Generation++
	if err := task.validateSeedGenerationRegistryPreflight(ctx, clusterTemplate, seedGeneration); err != nil {
		t.Fatal(err)
	}
	if pushChecks != 2 {
		t.Fatalf("changed PR generation did not rerun the registry push check: got %d", pushChecks)
	}
}

func TestValidateRegistryPushAccess(t *testing.T) {
	var pushAuthorized, uploadCancelled bool
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:team/seed:push" {
				t.Errorf("unexpected token scope %q", r.URL.Query().Get("scope"))
			}
			username, password, ok := r.BasicAuth()
			if !ok || username != "seed-user" || password != "seed-password" {
				t.Errorf("token request did not use the configured credentials")
			}
			_, _ = w.Write([]byte(`{"token":"push-token"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/team/seed/blobs/uploads/":
			if r.Header.Get("Authorization") == "Bearer push-token" {
				pushAuthorized = true
				w.Header().Set("Location", "/v2/team/seed/blobs/uploads/test-upload")
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("WWW-Authenticate", fmt.Sprintf("Bearer realm=%q,service=%q", server.URL+"/token", "test-registry"))
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/team/seed/blobs/uploads/test-upload":
			uploadCancelled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected registry request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	registry := strings.TrimPrefix(server.URL, "https://")
	image := registry + "/team/seed:4.22"
	auth := []byte(fmt.Sprintf(`{"auths":{%q:{"username":"wrong-user","password":"wrong-password"},%q:{"username":"seed-user","password":"seed-password"}}}`,
		registry, "https://"+registry+"/team"))
	if err := validateRegistryPushAccessWithClient(context.Background(), image, auth, server.Client()); err != nil {
		t.Fatal(err)
	}
	if !pushAuthorized || !uploadCancelled {
		t.Fatalf("push authorization=%t, upload cancellation=%t", pushAuthorized, uploadCancelled)
	}
}

func TestValidateRegistryPushAccessWithTokens(t *testing.T) {
	for _, tc := range []struct {
		name, field, token string
		exchanges          int32
		wantError          bool
	}{
		{name: "registry access token", field: "registrytoken", token: "push-token"},
		{name: "identity refresh token", field: "identitytoken", token: "refresh-token", exchanges: 1},
		{name: "rejected registry token", field: "registrytoken", token: "invalid-token", wantError: true},
		{name: "rejected identity token", field: "identitytoken", token: "invalid-token", exchanges: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var exchanges atomic.Int32
			var cancelled atomic.Bool
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/token":
					exchanges.Add(1)
					if r.Method != http.MethodPost || r.URL.Query().Get("tenant") != "example" ||
						r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Error("identity token did not use the OAuth POST exchange")
					}
					if err := r.ParseForm(); err != nil {
						t.Errorf("decode token form: %v", err)
					}
					if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("service") != "test-registry" ||
						r.PostForm.Get("scope") != "repository:team/seed:push" || r.PostForm.Get("client_id") != "oran-o2ims" {
						t.Error("incorrect OAuth exchange fields")
					}
					if r.PostForm.Get("refresh_token") != "refresh-token" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					_, _ = w.Write([]byte(`{"access_token":"push-token"}`))
				case r.Method == http.MethodPost && r.URL.Path == "/v2/team/seed/blobs/uploads/":
					if r.Header.Get("Authorization") == "Bearer refresh-token" {
						t.Error("refresh credential was sent to the registry")
					}
					if r.Header.Get("Authorization") == "Bearer push-token" {
						w.Header().Set("Location", "/v2/team/seed/blobs/uploads/test-upload")
						w.WriteHeader(http.StatusAccepted)
						return
					}
					w.Header().Set("WWW-Authenticate", fmt.Sprintf("Bearer realm=%q,service=%q", server.URL+"/token?tenant=example", "test-registry"))
					w.WriteHeader(http.StatusUnauthorized)
				case r.Method == http.MethodDelete && r.URL.Path == "/v2/team/seed/blobs/uploads/test-upload":
					cancelled.Store(r.Header.Get("Authorization") == "Bearer push-token")
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected registry request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			registry := strings.TrimPrefix(server.URL, "https://")
			auth := []byte(fmt.Sprintf(`{"auths":{%q:{%q:%q}}}`, registry+"/team", tc.field, tc.token))
			err := validateRegistryPushAccessWithClient(context.Background(), registry+"/team/seed:4.22", auth, server.Client())
			if (err != nil) != tc.wantError || exchanges.Load() != tc.exchanges || cancelled.Load() == tc.wantError {
				t.Fatalf("error=%v, exchanges=%d, cancelled=%t", err, exchanges.Load(), cancelled.Load())
			}
		})
	}
}

func TestSeedGenerationImageReferenceParsing(t *testing.T) {
	for _, tc := range []struct {
		image     string
		registry  string
		repo      string
		wantError bool
	}{
		{image: "busybox:latest", registry: "docker.io", repo: "library/busybox"},
		{image: "docker://quay.io/team/image:4.22", wantError: true},
		{image: "oci://localhost:5000/team/image", wantError: true},
		{image: "localhost:5000/team/image", registry: "localhost:5000", repo: "team/image"},
		{image: "quay.io/team/image:bad:tag", wantError: true},
		{image: "quay.io/team/../seed:tag", wantError: true},
		{image: "team/image:tag", registry: "docker.io", repo: "team/image"},
		{image: "invalid?image", wantError: true},
		{image: "https://quay.io/team/image", wantError: true},
	} {
		registry, repository, err := imageRepositoryFromPullSpec(tc.image)
		if (err != nil) != tc.wantError {
			t.Errorf("image=%q registry=%q repository=%q err=%v", tc.image, registry, repository, err)
			continue
		}
		if err == nil && (registry != tc.registry || repository != tc.repo) {
			t.Errorf("image=%q resolved to %s/%s, want %s/%s", tc.image, registry, repository, tc.registry, tc.repo)
		}
	}
}

func TestSeedGenerationRegistryPreflightUsesAdditionalTrustedCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "password" {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	registry := strings.TrimPrefix(server.URL, "https://")
	auth := []byte(fmt.Sprintf(`{"auths":{%q:{"username":"user","password":"password"}}}`, registry))
	if err := validateRegistryPushAccess(context.Background(), registry+"/team/seed:tag", auth); err == nil {
		t.Fatal("untrusted registry certificate was accepted")
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := validateRegistryPushAccessWithCA(context.Background(), registry+"/team/seed:tag", auth, ca); err != nil {
		t.Fatalf("registry with an additional trusted CA was rejected: %v", err)
	}
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
		{name: "permanent DNS failure", err: &net.DNSError{Err: "no such host", Name: "invalid.example", IsNotFound: true}, want: false},
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
		"seedAuth": {seedGenerationAuthSecretKey: secretValue},
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
		"seedAuth": {seedGenerationAuthSecretKey: []byte(`{"auths":{}}`)},
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
	if snapshot == nil || snapshot.Document.ClusterTemplateName != "seed-template.v1" || len(snapshot.Secrets) != 1 {
		t.Fatalf("complete snapshot was not adopted: %#v", snapshot)
	}
	actualUIDs := seedGenerationInputUIDs(snapshot)
	if len(actualUIDs) != 2 {
		t.Fatalf("got %d snapshot UIDs, want ConfigMap and Secret", len(actualUIDs))
	}
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.InputSnapshotResourceUIDs = actualUIDs
	if _, err := task.loadSeedGenerationInputSnapshot(ctx); err != nil {
		t.Fatalf("snapshot with matching UIDs was rejected: %v", err)
	}
	for name := range actualUIDs {
		actualUIDs[name] = "replacement-uid"
		break
	}
	if _, err := task.loadSeedGenerationInputSnapshot(ctx); err == nil || !strings.Contains(err.Error(), "UID does not match") {
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
		"seedAuth": {seedGenerationAuthSecretKey: []byte(`{"auths":{}}`)},
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
	if err := validateDockerAuthForRegistry(seedAuth, "docker.io"); err != nil {
		t.Fatalf("Docker Hub alias did not match: %v", err)
	}
	if err := validateDockerAuthForRegistry(seedAuth, "quay.io"); err == nil {
		t.Fatal("auth for another registry was accepted")
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
	auth := []byte(fmt.Sprintf(`{"auths":{%q:{"username":"seed-user","password":"seed-password"}}}`, registry))
	if err := validateRegistryPushAccessWithClient(context.Background(), image, auth, server.Client()); err != nil {
		t.Fatal(err)
	}
	if !pushAuthorized || !uploadCancelled {
		t.Fatalf("push authorization=%t, upload cancellation=%t", pushAuthorized, uploadCancelled)
	}
}

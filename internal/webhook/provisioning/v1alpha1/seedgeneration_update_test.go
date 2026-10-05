/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestValidateSeedGenerationUpdate(t *testing.T) {
	old := &provisioningv1alpha1.ProvisioningRequest{Spec: provisioningv1alpha1.ProvisioningRequestSpec{
		TemplateName:    "seed-template",
		TemplateVersion: "v1",
	}}
	old.Spec.TemplateParameters = runtime.RawExtension{Raw: []byte(`{"upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed:a","seedGenerationTimeout":"2h"}}}`)}
	started := metav1.Now()
	old.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{SeedGenerationStatus: &provisioningv1alpha1.SeedGenerationStatus{StartedAt: &started}}

	for _, tc := range []struct {
		name    string
		input   string
		blocked bool
	}{
		{"same semantic input", `{"upgradeParameters":{"seedGeneration":{"seedGenerationTimeout":"2h","seedImage":"quay.io/example/seed:a"}}}`, false},
		{"changed image", `{"upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed:b","seedGenerationTimeout":"2h"}}}`, true},
		{"removed seed key", `{"upgradeParameters":{}}`, true},
		{"changed timeout", `{"upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed:a","seedGenerationTimeout":"3h"}}}`, true},
		{"removed timeout", `{"upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed:a"}}}`, true},
		{"added unrelated parameter", `{"nodeClusterName":"other","upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed:a","seedGenerationTimeout":"2h"}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated := old.DeepCopy()
			updated.Spec.TemplateParameters.Raw = []byte(tc.input)
			err := validateSeedGenerationUpdate(old, updated)
			if tc.blocked && (err == nil || !strings.Contains(err.Error(), "cannot change")) {
				t.Fatalf("expected seed edit rejection, got %v", err)
			}
			if !tc.blocked && err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*provisioningv1alpha1.ProvisioningRequest)
	}{
		{"changed template name", func(pr *provisioningv1alpha1.ProvisioningRequest) { pr.Spec.TemplateName = "other-template" }},
		{"changed template version", func(pr *provisioningv1alpha1.ProvisioningRequest) { pr.Spec.TemplateVersion = "v2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated := old.DeepCopy()
			tc.mutate(updated)
			if err := validateSeedGenerationUpdate(old, updated); err == nil ||
				!strings.Contains(err.Error(), "switching ClusterTemplate") {
				t.Fatalf("expected ClusterTemplate switch rejection, got %v", err)
			}
		})
	}
	withoutTimeout := old.DeepCopy()
	withoutTimeout.Spec.TemplateParameters.Raw = []byte(`{"upgradeParameters":{"seedGeneration":{"seedImage":"quay.io/example/seed:a"}}}`)
	if err := validateSeedGenerationUpdate(withoutTimeout, old); err == nil {
		t.Fatal("adding a timeout after run start was accepted")
	}

	beforeStart := old.DeepCopy()
	beforeStart.Status.Extensions.ClusterDetails.SeedGenerationStatus.StartedAt = nil
	changed := old.DeepCopy()
	changed.Spec.TemplateParameters.Raw = []byte(`{"upgradeParameters":{}}`)
	if err := validateSeedGenerationUpdate(beforeStart, changed); err != nil {
		t.Fatalf("edit before run start was rejected: %v", err)
	}
	validator := &provisioningRequestValidator{}
	if _, err := validator.ValidateUpdate(context.Background(), old, changed); err == nil ||
		!strings.Contains(err.Error(), "cannot change") {
		t.Fatalf("webhook did not reject the active-run edit: %v", err)
	}
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"strings"
	"testing"

	bmhv1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSeedGenerationISOInstallInputsDerivedFromInstalledCluster(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "seed-cluster"}
	c := newSeedTestClient(t)
	if err := siteconfig.AddToScheme(c.Scheme()); err != nil {
		t.Fatal(err)
	}
	clusterInstance := &siteconfig.ClusterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "seed-cluster", Namespace: "seed-cluster"},
		Spec: siteconfig.ClusterInstanceSpec{
			ClusterImageSetNameRef: "ocp-4.22.15",
			Nodes: []siteconfig.NodeSpec{{RootDeviceHints: &bmhv1alpha1.RootDeviceHints{
				DeviceName: "/dev/disk/by-path/seed-disk",
			}}},
		},
	}
	imageSet := &hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ocp-4.22.15"},
		Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: "mirror.example/ocp-release:4.22.15-x86_64"},
	}
	if err := c.Create(ctx, clusterInstance); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, imageSet); err != nil {
		t.Fatal(err)
	}
	task := seedTestTask(pr, c)
	liveISO := map[string]any{"urlBase": "https://iso.example.test/ibi/"}
	document := &seedGenerationInputDocument{SeedGeneration: map[string]any{
		"liveISO": cloneSeedGenerationMap(liveISO),
	}}
	releaseImage, err := task.resolveISOInstallInputs(ctx, liveISO, document)
	if err != nil {
		t.Fatal(err)
	}
	if releaseImage != imageSet.Spec.ReleaseImage {
		t.Fatalf("release image = %q, want %q", releaseImage, imageSet.Spec.ReleaseImage)
	}
	frozen := document.SeedGeneration["liveISO"].(map[string]any)
	if frozen["releaseImage"] != imageSet.Spec.ReleaseImage || frozen["installationDisk"] != "/dev/disk/by-path/seed-disk" {
		t.Fatalf("snapshot did not capture installed cluster inputs: %v", frozen)
	}
	if _, present := liveISO["releaseImage"]; present {
		t.Fatal("derivation changed the merged template input")
	}
	if _, present := liveISO["installationDisk"]; present {
		t.Fatal("derivation changed the merged template input")
	}

	// An explicit target disk overrides the seed host's root device hint.
	liveISO["installationDisk"] = "/dev/disk/by-path/target-disk"
	document.SeedGeneration["liveISO"] = cloneSeedGenerationMap(liveISO)
	if _, err := task.resolveISOInstallInputs(ctx, liveISO, document); err != nil {
		t.Fatal(err)
	}
	if got := document.SeedGeneration["liveISO"].(map[string]any)["installationDisk"]; got != liveISO["installationDisk"] {
		t.Fatalf("target disk override was lost: %v", got)
	}
}

func TestSeedGenerationISOInstallInputsRequireDerivableDisk(t *testing.T) {
	ctx := context.Background()
	pr := &provisioningv1alpha1.ProvisioningRequest{}
	pr.Status.Extensions.ClusterDetails = &provisioningv1alpha1.ClusterDetails{Name: "seed-cluster"}
	c := newSeedTestClient(t)
	if err := siteconfig.AddToScheme(c.Scheme()); err != nil {
		t.Fatal(err)
	}
	clusterInstance := &siteconfig.ClusterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "seed-cluster", Namespace: "seed-cluster"},
		Spec: siteconfig.ClusterInstanceSpec{Nodes: []siteconfig.NodeSpec{{RootDeviceHints: &bmhv1alpha1.RootDeviceHints{
			WWN: "seed-host-specific-wwn",
		}}}},
	}
	if err := c.Create(ctx, clusterInstance); err != nil {
		t.Fatal(err)
	}
	liveISO := map[string]any{"releaseImage": "mirror.example/ocp-release:4.22.15-x86_64"}
	document := &seedGenerationInputDocument{SeedGeneration: map[string]any{
		"liveISO": cloneSeedGenerationMap(liveISO),
	}}
	task := seedTestTask(pr, c)
	_, err := task.resolveISOInstallInputs(ctx, liveISO, document)
	if err == nil || !strings.Contains(err.Error(), "set it explicitly for the target servers") {
		t.Fatalf("missing device path did not produce an actionable error: %v", err)
	}

	// Explicit values do not require the ClusterInstance to be available.
	liveISO["installationDisk"] = "/dev/disk/by-path/target-disk"
	document.SeedGeneration["liveISO"] = cloneSeedGenerationMap(liveISO)
	if err := c.Delete(ctx, clusterInstance); err != nil {
		t.Fatal(err)
	}
	if _, err := task.resolveISOInstallInputs(ctx, liveISO, document); err != nil {
		t.Fatalf("explicit installation inputs unexpectedly required a ClusterInstance: %v", err)
	}
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/yaml"
)

func seedRecoveryTestSnapshot(t *testing.T, pr *provisioningv1alpha1.ProvisioningRequest) (*seedGenerationInputSnapshot, client.Client) {
	t.Helper()
	document := seedGenerationLifecycleTestSnapshot().Document
	document.ClusterTemplateName, document.ClusterTemplateNamespace = "seed-template", "templates"
	config, secrets, err := makeSeedGenerationSnapshotObjects(pr, document, map[string]map[string][]byte{
		"seedAuth": {"seedAuth": []byte(`{"auths":{}}`)}, "spokeKubeconfig": {"kubeconfig": []byte("test")},
	})
	if err != nil {
		t.Fatal(err)
	}
	config.UID = "config-uid"
	objects := []client.Object{pr, config}
	for key, secret := range secrets {
		secret.UID = types.UID(key + "-uid")
		objects = append(objects, secret)
	}
	snapshot := &seedGenerationInputSnapshot{Document: document, ConfigMap: config, Secrets: secrets}
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.InputSnapshotResourceUIDs = seedGenerationInputUIDs(snapshot)
	return snapshot, newSeedTestClient(t, objects...)
}

func TestSeedGenerationPinnedSnapshotLossPreservesRemainingCredentials(t *testing.T) {
	ctx := context.Background()
	pr := seedDetachmentTestRequest()
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.StartedAt = &metav1.Time{Time: time.Now()}
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.TimeoutSeconds = 3600
	snapshot, c := seedRecoveryTestSnapshot(t, pr)
	if err := c.Delete(ctx, snapshot.Secrets["seedAuth"]); err != nil {
		t.Fatal(err)
	}
	task := seedTestTask(pr, c)
	if _, err := task.reconcileSeedGeneration(ctx); !errors.Is(err, errSeedGenerationSnapshotUnavailable) || !isSeedGenerationTransientError(err) {
		t.Fatalf("expected retryable snapshot error, got %v", err)
	}
	if seedGenerationTerminal(pr) {
		t.Fatal("a missing cached snapshot prematurely failed the run")
	}
	// An unavailable snapshot must not extend the persisted run deadline.
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.StartedAt = &metav1.Time{Time: time.Now().Add(-2 * time.Hour)}
	if err := c.Status().Update(ctx, pr); err != nil {
		t.Fatal(err)
	}
	if _, err := task.reconcileSeedGeneration(ctx); !errors.Is(err, errSeedGenerationSnapshotUnavailable) {
		t.Fatalf("expected cleanup to wait for the unavailable snapshot after timeout, got %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pr), pr); err != nil {
		t.Fatal(err)
	}
	if condition := seedGenerationCondition(pr); condition == nil || condition.Reason != string(provisioningv1alpha1.CRconditionReasons.TimedOut) {
		t.Fatalf("missing snapshot bypassed the run deadline: %+v", condition)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(snapshot.Secrets["seedAuth"]), &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Fatalf("missing pinned credentials were recreated: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(snapshot.Secrets["spokeKubeconfig"]), &corev1.Secret{}); err != nil {
		t.Fatalf("last spoke credential was removed: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(snapshot.ConfigMap), &corev1.ConfigMap{}); err != nil {
		t.Fatalf("pinned input document was removed: %v", err)
	}
}

func TestSeedGenerationPinnedSnapshotRecoversFromCacheGaps(t *testing.T) {
	for _, detached := range []bool{false, true} {
		for _, gap := range []string{"config", "credential", "credentials", "all"} {
			t.Run(fmt.Sprintf("detached=%t/gap=%s", detached, gap), func(t *testing.T) {
				ctx := context.Background()
				pr := seedDetachmentTestRequest()
				status := pr.Status.Extensions.ClusterDetails.SeedGenerationStatus
				startedAt := metav1.NewTime(time.Now().Truncate(time.Second))
				status.StartedAt, status.TimeoutSeconds = &startedAt, 3600
				status.DetachmentStarted, status.AgentDetachmentCompleted = detached, detached
				reason := provisioningv1alpha1.CRconditionReasons.Validating
				if detached {
					reason = provisioningv1alpha1.CRconditionReasons.InProgress
				}
				setSeedGenerationProgress(pr, reason, "Seed generation is in progress")
				snapshot, base := seedRecoveryTestSnapshot(t, pr)
				hide := true
				hub := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if err := c.List(ctx, list, opts...); err != nil {
							return fmt.Errorf("list intercepted resources: %w", err)
						}
						if hide {
							switch items := list.(type) {
							case *corev1.ConfigMapList:
								if gap == "config" || gap == "all" {
									items.Items = nil
								}
							case *corev1.SecretList:
								if gap == "credentials" || gap == "all" {
									items.Items = nil
								} else if gap == "credential" && len(items.Items) > 0 {
									items.Items = items.Items[:1]
								}
							}
						}
						return nil
					},
					Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
						t.Fatal("a pinned snapshot must not be recreated")
						return nil
					},
					Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
						t.Fatal("a pinned snapshot must not be deleted")
						return nil
					},
				})
				task := seedTestTask(pr, hub)
				if _, err := task.reconcileSeedGeneration(ctx); !errors.Is(err, errSeedGenerationSnapshotUnavailable) || !isSeedGenerationTransientError(err) {
					t.Fatalf("cache gap was not retryable: %v", err)
				}
				if err := hub.Get(ctx, client.ObjectKeyFromObject(pr), pr); err != nil {
					t.Fatal(err)
				}
				status = pr.Status.Extensions.ClusterDetails.SeedGenerationStatus
				if seedGenerationTerminal(pr) || !status.StartedAt.Equal(&startedAt) || status.TimeoutSeconds != 3600 ||
					!seedGenerationSnapshotUIDsEqual(status.InputSnapshotResourceUIDs, seedGenerationInputUIDs(snapshot)) {
					t.Fatalf("cache gap changed the persisted run: %+v", status)
				}
				hide = false
				task = seedTestTask(pr, hub)
				recovered, retry, err := task.ensureSeedGenerationInputSnapshot(ctx, nil)
				if err != nil || retry || recovered == nil ||
					!seedGenerationSnapshotUIDsEqual(seedGenerationInputUIDs(snapshot), seedGenerationInputUIDs(recovered)) {
					t.Fatalf("pinned inputs were not recovered: retry=%t err=%v", retry, err)
				}
				if detached {
					spoke := newSeedLifecycleSpokeClient(t)
					task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
					if _, err := task.reconcileSeedGeneration(ctx); err != nil {
						t.Fatal(err)
					}
					if err := spoke.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedCRName}, newSeedGenerationSeedGenerator()); err != nil {
						t.Fatalf("seed generation did not resume after the cache recovered: %v", err)
					}
				}
			})
		}
	}
}

func TestSeedGenerationSnapshotCreationDoesNotRequireCacheReadAfterWrite(t *testing.T) {
	pr := seedDetachmentTestRequest()
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachmentStarted = false
	base := newSeedTestClient(t, pr).(client.WithWatch)
	creates, deletes := 0, 0
	c := interceptor.NewClient(base, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return nil },
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			creates++
			obj.SetUID(types.UID(obj.GetName() + "-uid"))
			return c.Create(ctx, obj, opts...)
		},
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			deletes++
			return nil
		},
	})
	doc := seedGenerationLifecycleTestSnapshot().Document
	snapshot, err := seedTestTask(pr, c).createSeedGenerationInputSnapshot(context.Background(), doc,
		map[string]map[string][]byte{"seedAuth": {"seedAuth": []byte("auth")}, "spokeKubeconfig": {"kubeconfig": []byte("config")}})
	if err != nil || snapshot == nil || creates != 3 || deletes != 0 {
		t.Fatalf("cache lag broke snapshot creation: creates=%d deletes=%d err=%v", creates, deletes, err)
	}
	for _, uid := range seedGenerationInputUIDs(snapshot) {
		if uid == "" {
			t.Fatal("server UID was not recorded")
		}
	}
}

func TestSeedGenerationRetriesThrottledSubmission(t *testing.T) {
	for _, rejections := range []int{1, 2} {
		t.Run(fmt.Sprintf("rejections=%d", rejections), func(t *testing.T) {
			ctx := context.Background()
			pr := seedDetachmentTestRequest()
			hub := newSeedTestClient(t, pr)
			baseSpoke := newSeedLifecycleSpokeClient(t)
			attempts, secretCreates := 0, 0
			spoke := interceptor.NewClient(baseSpoke, interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if obj.GetName() == seedGenerationLCASeedCRName {
						attempts++
						persisted := &provisioningv1alpha1.ProvisioningRequest{}
						if err := hub.Get(ctx, client.ObjectKeyFromObject(pr), persisted); err != nil {
							t.Fatal(err)
						}
						if !persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted {
							t.Fatal("submission intent was not persisted before create")
						}
						if attempts <= rejections {
							return apierrors.NewTooManyRequests("API request queue is full", 1)
						}
					} else {
						secretCreates++
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			for attempt := 0; attempt <= rejections; attempt++ {
				// Reconstruct the task from persisted status on every retry.
				if err := hub.Get(ctx, client.ObjectKeyFromObject(pr), pr); err != nil {
					t.Fatal(err)
				}
				task := seedTestTask(pr, hub)
				task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
				_, err := task.reconcileSeedGenerator(ctx, seedGenerationLifecycleTestSnapshot())
				if attempt < rejections {
					if !apierrors.IsTooManyRequests(err) {
						t.Fatalf("expected throttled create, got %v", err)
					}
				} else if err != nil {
					t.Fatalf("submission did not recover from throttling: %v", err)
				}
				if err := hub.Get(ctx, client.ObjectKeyFromObject(pr), pr); err != nil {
					t.Fatal(err)
				}
				if submitted := pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted; submitted != (attempt == rejections) {
					t.Fatalf("attempt %d: persisted submission guard=%t", attempt, submitted)
				}
				if attempt < rejections {
					if condition := seedGenerationCondition(pr); condition == nil ||
						condition.Message != "Waiting to retry SeedGenerator submission after API throttling" ||
						pr.Status.ProvisioningStatus.ProvisioningDetails != condition.Message {
						t.Fatal("throttled submission progress was not persisted on both status surfaces")
					}
				}
				if seedGenerationTerminal(pr) {
					t.Fatal("throttled create permanently failed the run")
				}
			}
			if err := baseSpoke.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedCRName}, newSeedGenerationSeedGenerator()); err != nil {
				t.Fatalf("SeedGenerator was not created after retry: %v", err)
			}
			if attempts != rejections+1 || secretCreates != 1 {
				t.Fatalf("unexpected creates: attempts=%d secrets=%d", attempts, secretCreates)
			}
		})
	}
}

func TestSeedGenerationSubmissionResponseLossDoesNotCreateAnotherRun(t *testing.T) {
	ctx := context.Background()
	pr := seedDetachmentTestRequest()
	hub := newSeedTestClient(t, pr)
	baseSpoke := newSeedLifecycleSpokeClient(t)
	creates := 0
	spoke := interceptor.NewClient(baseSpoke, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		creates++
		if err := c.Create(ctx, obj, opts...); err != nil {
			return fmt.Errorf("create intercepted resource: %w", err)
		}
		if obj.GetName() != seedGenerationLCASeedCRName {
			return nil
		}
		persisted := &provisioningv1alpha1.ProvisioningRequest{}
		if err := hub.Get(ctx, client.ObjectKeyFromObject(pr), persisted); err != nil {
			t.Fatal(err)
		}
		if !persisted.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted {
			t.Fatal("submission intent was not durable before creating the LCA resource")
		}
		if condition := seedGenerationCondition(persisted); condition == nil ||
			condition.Message != "Starting LCA seed image generation and push" ||
			persisted.Status.ProvisioningStatus.ProvisioningDetails != condition.Message {
			t.Fatal("LCA handoff progress was not durable before creating the SeedGenerator")
		}
		// LCA consumes both objects before our client receives the create response.
		if err := c.Delete(ctx, obj); err != nil {
			t.Fatal(err)
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: seedGenerationLCASeedSecretName, Namespace: seedGenerationLCAOperatorNamespace}}
		if err := c.Delete(ctx, secret); err != nil {
			t.Fatal(err)
		}
		return apierrors.NewTimeoutError("response lost", 0)
	}})
	task := seedTestTask(pr, hub)
	task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
	if _, err := task.reconcileSeedGenerator(ctx, seedGenerationLifecycleTestSnapshot()); err == nil {
		t.Fatal("expected lost response")
	}
	if err := hub.Get(ctx, client.ObjectKeyFromObject(pr), pr); err != nil {
		t.Fatal(err)
	}
	if _, err := task.reconcileSeedGenerator(ctx, seedGenerationLifecycleTestSnapshot()); err != nil {
		t.Fatal(err)
	}
	if creates != 2 {
		t.Fatalf("LCA objects were recreated after restart: %d creates", creates)
	}
}

func TestSeedGenerationCleanupWaitsForLCARestore(t *testing.T) {
	ctx := context.Background()
	pr := seedDetachmentTestRequest()
	pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.SeedGeneratorSubmitted = true
	snapshot, hub := seedRecoveryTestSnapshot(t, pr)
	spoke := newSeedLifecycleSpokeClient(t)
	task := seedTestTask(pr, hub)
	task.seedGenerationSpokeClientBuilder = func([]byte) (client.Client, error) { return spoke, nil }
	for _, state := range []string{"absent", "active", "finished"} {
		if state == "active" {
			if err := spoke.Create(ctx, desiredSeedGenerator(pr, snapshot)); err != nil {
				t.Fatal(err)
			}
		}
		if state == "finished" {
			seed := newSeedGenerationSeedGenerator()
			if err := spoke.Get(ctx, client.ObjectKey{Name: seedGenerationLCASeedCRName}, seed); err != nil {
				t.Fatal(err)
			}
			seed.Object["status"] = map[string]any{"conditions": []any{map[string]any{
				"type": seedGenerationLCACompleted, "status": "False", "reason": "Failed",
			}}}
			if err := spoke.Update(ctx, seed); err != nil {
				t.Fatal(err)
			}
		}
		pending, err := task.cleanupSeedGenerationDetachedResourcesAndRecordCompletion(ctx)
		if err != nil || !pending {
			t.Fatalf("state=%s pending=%t err=%v", state, pending, err)
		}
		if pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachedCleanupCompleted {
			t.Fatal("marked clean while LCA may restore its credentials")
		}
	}
	pending, err := task.cleanupSeedGenerationDetachedResourcesAndRecordCompletion(ctx)
	if err != nil || pending || !pr.Status.Extensions.ClusterDetails.SeedGenerationStatus.DetachedCleanupCompleted {
		t.Fatalf("cleanup did not finish after LCA termination: pending=%t err=%v", pending, err)
	}
}

func TestSeedGenerationObservabilityCleanupPreservesCustomMonitoring(t *testing.T) {
	for _, tc := range []struct{ namespace, name, component, field string }{
		{"openshift-monitoring", "cluster-monitoring-config", "prometheusK8s", "additionalAlertManagerConfigs"},
		{"openshift-user-workload-monitoring", "user-workload-monitoring-config", "prometheus", "additionalAlertmanagerConfigs"},
	} {
		t.Run(tc.namespace, func(t *testing.T) {
			config := map[string]any{tc.component: map[string]any{"retention": "12h", tc.field: []any{
				map[string]any{"tlsConfig": map[string]any{"ca": map[string]any{"name": "hub-alertmanager-router-ca-hub"}}},
				map[string]any{"tlsConfig": map[string]any{"ca": map[string]any{"name": "user-ca"}}},
			}}, "unrelated": map[string]any{"enabled": true}}
			encoded, err := yaml.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: tc.namespace}, Data: map[string]string{"config.yaml": string(encoded), "other": "preserved"}}
			c := newSeedLifecycleSpokeClient(t, cm)
			if err := cleanupSeedGenerationMonitoringConfig(context.Background(), c, tc.namespace); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(cm), cm); err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &config); err != nil {
				t.Fatal(err)
			}
			entries, _, _ := unstructured.NestedSlice(config, tc.component, tc.field)
			retention, _, _ := unstructured.NestedString(config, tc.component, "retention")
			if len(entries) != 1 || retention != "12h" || cm.Data["other"] != "preserved" || config["unrelated"] == nil {
				t.Fatalf("monitoring settings were lost: %s", cm.Data["config.yaml"])
			}
		})
	}
}

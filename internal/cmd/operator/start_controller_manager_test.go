/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestControllerManagerCacheScopesSeedResources(t *testing.T) {
	for _, tc := range []struct {
		kind, resource string
		groupVersion   schema.GroupVersion
		list           client.ObjectList
	}{
		{"Job", "jobs", batchv1.SchemeGroupVersion, &batchv1.JobList{}},
		{"Pod", "pods", corev1.SchemeGroupVersion, &corev1.PodList{}},
		{"PersistentVolumeClaim", "persistentvolumeclaims", corev1.SchemeGroupVersion, &corev1.PersistentVolumeClaimList{}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			const namespace = "custom-operator-namespace"
			apiPath := "/apis/" + tc.groupVersion.String()
			if tc.groupVersion.Group == "" {
				apiPath = "/api/" + tc.groupVersion.Version
			}
			resourcePath := fmt.Sprintf("%s/namespaces/%s/%s", apiPath, namespace, tc.resource)
			watchStarted := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != resourcePath {
					t.Errorf("request exceeds namespaced read permissions: %s %s", r.Method, r.URL.Path)
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				if selector := r.URL.Query().Get("labelSelector"); selector != "" {
					t.Errorf("cache must also expose resources without seed-generation labels, got selector %q", selector)
				}
				w.Header().Set("Content-Type", "application/json")
				encoder := json.NewEncoder(w)
				object := map[string]any{
					"apiVersion": tc.groupVersion.String(), "kind": tc.kind,
					"metadata": map[string]any{"name": "ordinary-resource", "namespace": namespace, "resourceVersion": "1"},
				}
				if r.URL.Query().Get("watch") == "true" {
					// Support streaming initial lists as well as ordinary LIST/WATCH.
					if r.URL.Query().Get("sendInitialEvents") == "true" {
						if err := encoder.Encode(map[string]any{"type": "ADDED", "object": object}); err != nil {
							t.Errorf("write initial watch event: %v", err)
						}
						object["metadata"] = map[string]any{
							"resourceVersion": "1", "annotations": map[string]string{"k8s.io/initial-events-end": "true"},
						}
						if err := encoder.Encode(map[string]any{"type": "BOOKMARK", "object": object}); err != nil {
							t.Errorf("write watch bookmark: %v", err)
						}
					}
					w.(http.Flusher).Flush()
					select {
					case watchStarted <- struct{}{}:
					default:
					}
					<-r.Context().Done()
					return
				}
				if err := encoder.Encode(map[string]any{
					"apiVersion": tc.groupVersion.String(), "kind": tc.kind + "List",
					"metadata": map[string]string{"resourceVersion": "1"}, "items": []any{object},
				}); err != nil {
					t.Errorf("write list response: %v", err)
				}
			}))
			defer server.Close()

			options := controllerManagerCacheOptions(namespace)
			options.Scheme = scheme
			mapper := meta.NewDefaultRESTMapper(nil)
			for object := range options.ByObject {
				gvks, _, err := scheme.ObjectKinds(object)
				if err != nil {
					t.Fatal(err)
				}
				mapper.Add(gvks[0], meta.RESTScopeNamespace)
			}
			mapper.Add(tc.groupVersion.WithKind(tc.kind), meta.RESTScopeNamespace)
			mapper.AddSpecific(tc.groupVersion.WithKind(tc.kind+"List"), tc.groupVersion.WithResource(tc.resource),
				tc.groupVersion.WithResource(tc.resource), meta.RESTScopeNamespace)
			options.Mapper = mapper
			reader, err := cache.New(&rest.Config{Host: server.URL}, options)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := reader.GetInformerForKind(ctx, tc.groupVersion.WithKind(tc.kind)); err != nil {
				t.Fatal(err)
			}
			stopped := make(chan error, 1)
			go func() { stopped <- reader.Start(ctx) }()
			defer func() {
				cancel()
				select {
				case err := <-stopped:
					if err != nil {
						t.Errorf("stop cache: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Error("cache did not stop")
				}
			}()
			if !reader.WaitForCacheSync(ctx) {
				t.Fatal("cache did not sync using namespaced read permissions")
			}
			if err := reader.List(ctx, tc.list, client.InNamespace(namespace)); err != nil {
				t.Fatalf("namespaced cached read failed: %v", err)
			}
			objects, err := meta.ExtractList(tc.list)
			if err != nil || len(objects) != 1 {
				t.Fatalf("resource without seed-generation labels was not cached: count=%d err=%v", len(objects), err)
			}
			select {
			case <-watchStarted:
			case <-ctx.Done():
				t.Fatal("namespaced watch was not started")
			}
			if err := reader.List(ctx, tc.list, client.InNamespace("another-namespace")); err == nil {
				t.Fatal("cache allowed a read outside the operator namespace")
			}
		})
	}
}

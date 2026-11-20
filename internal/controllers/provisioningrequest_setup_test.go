/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"testing"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestProvisioningRequestUpdatePredicate(t *testing.T) {
	tests := []struct {
		name string
		old  *provisioningv1alpha1.ProvisioningRequest
		new  *provisioningv1alpha1.ProvisioningRequest
		want bool
	}{
		{
			name: "generation change",
			old:  &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new:  &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Generation: 2}},
			want: true,
		},
		{
			name: "skip cleanup annotation change",
			old:  &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new: &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				Annotations: map[string]string{
					ctlrutils.SkipCleanupAnnotation: "",
				},
			}},
			want: true,
		},
		{
			name: "deletion starts without generation change",
			old:  &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new: &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
				Generation:        1,
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
			}},
			want: true,
		},
		{
			name: "unrelated annotation change",
			old:  &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			new: &provisioningv1alpha1.ProvisioningRequest{ObjectMeta: metav1.ObjectMeta{
				Generation:  1,
				Annotations: map[string]string{"example.com/annotation": "value"},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := provisioningRequestUpdatePredicate(event.UpdateEvent{
				ObjectOld: tt.old,
				ObjectNew: tt.new,
			}); got != tt.want {
				t.Fatalf("provisioningRequestUpdatePredicate() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestManagedClusterUpdatePredicate(t *testing.T) {
	versionLabel := ctlrutils.OpenshiftVersionLabelName
	newCluster := func(available bool, labels map[string]string) *clusterv1.ManagedCluster {
		status := metav1.ConditionFalse
		if available {
			status = metav1.ConditionTrue
		}
		return &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Status: clusterv1.ManagedClusterStatus{
				Conditions: []metav1.Condition{
					{Type: clusterv1.ManagedClusterConditionAvailable, Status: status},
				},
			},
		}
	}

	tests := []struct {
		name string
		old  *clusterv1.ManagedCluster
		new  *clusterv1.ManagedCluster
		want bool
	}{
		{
			name: "availability change",
			old:  newCluster(false, nil),
			new:  newCluster(true, nil),
			want: true,
		},
		{
			name: "openshiftVersion label change",
			old:  newCluster(true, map[string]string{versionLabel: "4.16.0"}),
			new:  newCluster(true, map[string]string{versionLabel: "4.17.0"}),
			want: true,
		},
		{
			name: "openshiftVersion label added",
			old:  newCluster(true, nil),
			new:  newCluster(true, map[string]string{versionLabel: "4.17.0"}),
			want: true,
		},
		{
			name: "unrelated label change",
			old:  newCluster(true, map[string]string{versionLabel: "4.17.0"}),
			new:  newCluster(true, map[string]string{versionLabel: "4.17.0", "example.com/label": "value"}),
			want: false,
		},
		{
			name: "no change",
			old:  newCluster(true, map[string]string{versionLabel: "4.17.0"}),
			new:  newCluster(true, map[string]string{versionLabel: "4.17.0"}),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := managedClusterUpdatePredicate(event.UpdateEvent{
				ObjectOld: tt.old,
				ObjectNew: tt.new,
			}); got != tt.want {
				t.Fatalf("managedClusterUpdatePredicate() = %t, want %t", got, tt.want)
			}
		})
	}
}

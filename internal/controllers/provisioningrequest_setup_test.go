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

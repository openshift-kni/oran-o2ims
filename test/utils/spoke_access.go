/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package teste2eutils

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SimulateSpokeAccessReady waits for the controller to create the MSA and ManifestWork,
// then stands in for the addon and work agents by creating the token secret, setting
// the MSA tokenSecretRef, and marking the ManifestWork applied and available.
func SimulateSpokeAccessReady(
	ctx context.Context,
	k8sClient client.Client,
	clusterName, msaName, mwName string,
	timeout, interval time.Duration,
) {
	tokenSecretName := msaName + "-token"

	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: msaName, Namespace: clusterName},
			&msav1beta1.ManagedServiceAccount{})
	}, timeout, interval).Should(Succeed(), "MSA should be created by the controller")

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: clusterName},
		Data:       map[string][]byte{"token": []byte("test-token"), "ca.crt": []byte("test-ca")},
	}
	Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, tokenSecret))).To(Succeed())

	msa := &msav1beta1.ManagedServiceAccount{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: msaName, Namespace: clusterName}, msa)).To(Succeed())
	msa.Status.TokenSecretRef = &msav1beta1.SecretRef{
		Name:                 tokenSecretName,
		LastRefreshTimestamp: metav1.Now(),
	}
	Expect(k8sClient.Status().Update(ctx, msa)).To(Succeed())

	// Stand in for the work agent: report the current generation of the work as
	// both applied and available, since EnsureSpokeClient only accepts
	// conditions whose observedGeneration matches. This is reported once, on
	// the first attempt that finds the work and updates it; nothing refreshes
	// it afterwards, so a controller that kept re-patching the spec would leave
	// these conditions behind and the caller would never become ready.
	//
	// That is deliberate, and it bounds where this helper can be used: a test
	// that changes the requested RBAC rules after calling it bumps the
	// generation the same way, strands these conditions on the old one, and
	// hangs at not-ready. Such a test has to report the conditions again for
	// the new generation rather than reuse this call.
	Eventually(func() error {
		mw := &workv1.ManifestWork{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: mwName, Namespace: clusterName}, mw); err != nil {
			return fmt.Errorf("failed to get ManifestWork %s: %w", mwName, err)
		}
		for _, conditionType := range []string{workv1.WorkApplied, workv1.WorkAvailable} {
			meta.SetStatusCondition(&mw.Status.Conditions, metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionTrue,
				Reason:             "Applied",
				ObservedGeneration: mw.Generation,
			})
		}
		return k8sClient.Status().Update(ctx, mw)
	}, timeout, interval).Should(Succeed(), "ManifestWork should be created by the controller and marked ready")
}

func AssertSpokeAccessCleaned(
	ctx context.Context,
	k8sClient client.Client,
	clusterName, msaName, mwName string,
	timeout, interval time.Duration,
) {
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: msaName, Namespace: clusterName},
			&msav1beta1.ManagedServiceAccount{})
		return errors.IsNotFound(err)
	}, timeout, interval).Should(BeTrue(), "MSA should be deleted")

	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: mwName, Namespace: clusterName},
			&workv1.ManifestWork{})
		return errors.IsNotFound(err)
	}, timeout, interval).Should(BeTrue(), "ManifestWork should be deleted")
}

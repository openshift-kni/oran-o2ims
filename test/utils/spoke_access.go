/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package teste2eutils

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SimulateSpokeAccessReady waits for the controller to create the MSA and ManifestWork,
// then simulates the addon controller by creating the token secret and setting
// the MSA tokenSecretRef and MW Available status.
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

	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: mwName, Namespace: clusterName},
			&workv1.ManifestWork{})
	}, timeout, interval).Should(Succeed(), "ManifestWork should be created by the controller")

	mw := &workv1.ManifestWork{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mwName, Namespace: clusterName}, mw)).To(Succeed())
	mw.Status.Conditions = []metav1.Condition{
		{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue, Reason: "Applied", LastTransitionTime: metav1.Now()},
	}
	Expect(k8sClient.Status().Update(ctx, mw)).To(Succeed())
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

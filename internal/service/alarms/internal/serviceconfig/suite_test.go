/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package serviceconfig_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8senvtest "sigs.k8s.io/controller-runtime/pkg/envtest"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

const testNamespace = "alarms-serviceconfig-test"

var (
	testEnv   *k8senvtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	scheme    *runtime.Scheme
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestServiceConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alarms ServiceConfig Envtest Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	slog.SetDefault(slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{Level: slog.LevelInfo})))

	scheme = runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(appsv1.AddToScheme(scheme)).To(Succeed())
	Expect(batchv1.AddToScheme(scheme)).To(Succeed())
	Expect(rbacv1.AddToScheme(scheme)).To(Succeed())
	Expect(admissionregistrationv1.AddToScheme(scheme)).To(Succeed())

	testEnv = &k8senvtest.Environment{Scheme: scheme}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())

	// Namespace that hosts the alarms-server deployment and the cleanup resources.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	// EnsureCleanupCronJob takes ownership of the resources it creates by setting
	// an owner reference to the alarms-server deployment, so it must exist.
	Expect(k8sClient.Create(ctx, newAlarmsServerDeployment())).To(Succeed())
})

var _ = AfterSuite(func() {
	cancel()
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

// newAlarmsServerDeployment returns a minimal but valid alarms-server Deployment
// used as the owner of the cleanup ConfigMap and CronJob.
func newAlarmsServerDeployment() *appsv1.Deployment {
	labels := map[string]string{"app": ctlrutils.InventoryAlarmServerName}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctlrutils.InventoryAlarmServerName,
			Namespace: testNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "alarms-server",
						Image: "registry.example.com/alarms-server:test",
					}},
				},
			},
		},
	}
}

// loadValidatingAdmissionPolicy decodes the shipped VAP + binding manifest so the
// tests exercise the exact CEL that is deployed, rather than a copy.
func loadValidatingAdmissionPolicy() (*admissionregistrationv1.ValidatingAdmissionPolicy, *admissionregistrationv1.ValidatingAdmissionPolicyBinding) {
	path := filepath.Join("..", "..", "..", "..", "..", "config", "rbac", "validatingadmissionpolicy_alarms_server.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // fixed in-repo test fixture path
	Expect(err).ToNot(HaveOccurred())

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)

	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
	Expect(decoder.Decode(policy)).To(Succeed())

	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	Expect(decoder.Decode(binding)).To(Succeed())

	return policy, binding
}

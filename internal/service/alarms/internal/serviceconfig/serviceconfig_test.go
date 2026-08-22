/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package serviceconfig_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/serviceconfig"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

const (
	cleanupCronJobName   = "alarms-server-events-cleanup"
	cleanupConfigMapName = "alarms-server-events-cleanup-sql"

	// alarmsServerUser is the impersonated username the ValidatingAdmissionPolicy
	// matches. The SA short name is fixed because the kustomize namePrefix is.
	alarmsServerUser = "system:serviceaccount:" + testNamespace + ":oran-o2ims-alarms-server"

	// otherNamespace is a second namespace used to prove the policy matches the
	// alarms-server SA regardless of the CronJob's namespace.
	otherNamespace = "alarms-serviceconfig-other"
)

// newServiceConfig returns a Config wired to the envtest API server.
func newServiceConfig() *serviceconfig.Config {
	return &serviceconfig.Config{
		PostgresImage: "registry.example.com/postgres:test",
		PodNamespace:  testNamespace,
		PgConnConfig: db.PgConfig{
			Host:     "postgres-server",
			Port:     "5432",
			User:     "alarms",
			Database: "alarms",
		},
		HubClient: k8sClient,
	}
}

var _ = Describe("EnsureCleanupCronJob", Label("envtest"), func() {
	It("creates and then updates the cleanup ConfigMap and CronJob using only get/create/update", func() {
		c := newServiceConfig()
		sc := &models.ServiceConfiguration{RetentionPeriod: 10}

		// First call creates the resources (exercises get + create).
		Expect(c.EnsureCleanupCronJob(ctx, sc)).To(Succeed())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: cleanupConfigMapName}, cm)).To(Succeed())
		Expect(cm.Data).To(HaveKey("cleanup.sql"))

		cronJob := &batchv1.CronJob{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: cleanupCronJobName}, cronJob)).To(Succeed())

		// The deployed cleanup CronJob must not pin a ServiceAccount so it runs as
		// the namespace 'default' SA (zero RBAC) and is admitted by the policy.
		Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())

		// Second call updates the existing resources (exercises get + update). This
		// proves that removing list/watch/delete/patch from the Role does not break
		// EnsureCleanupCronJob, which backs startup and the AlarmServiceConfiguration
		// PATCH/PUT endpoints.
		sc.RetentionPeriod = 20
		Expect(c.EnsureCleanupCronJob(ctx, sc)).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: cleanupCronJobName}, cronJob)).To(Succeed())
		Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())
	})
})

var _ = Describe("alarms-server CronJob admission policy", Label("envtest"), Ordered, func() {
	var alarmsClient client.Client

	BeforeAll(func() {
		// Grant the alarms-server SA the same trimmed cronjobs verbs the shipped
		// Role has (get/create/update) so RBAC permits the create and the
		// ValidatingAdmissionPolicy is what constrains the pod ServiceAccount.
		Expect(k8sClient.Create(ctx, cronJobRole(testNamespace))).To(Succeed())
		Expect(k8sClient.Create(ctx, cronJobRoleBinding(testNamespace))).To(Succeed())

		// A second namespace where the alarms-server SA (from testNamespace) is
		// also allowed to create CronJobs, used to prove the policy matches the SA
		// regardless of the CronJob's namespace.
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: otherNamespace}})).To(Succeed())
		Expect(k8sClient.Create(ctx, cronJobRole(otherNamespace))).To(Succeed())
		Expect(k8sClient.Create(ctx, cronJobRoleBinding(otherNamespace))).To(Succeed())

		policy, policyBinding := loadValidatingAdmissionPolicy()
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		Expect(k8sClient.Create(ctx, policyBinding)).To(Succeed())

		impersonated := rest.CopyConfig(cfg)
		impersonated.Impersonate = rest.ImpersonationConfig{UserName: alarmsServerUser}
		var err error
		alarmsClient, err = client.New(impersonated, client.Options{Scheme: scheme})
		Expect(err).ToNot(HaveOccurred())
	})

	It("denies a CronJob whose pod runs as a non-default ServiceAccount", func() {
		// The policy takes a moment to become active after the binding is created,
		// so retry until it is enforced. If a create unexpectedly succeeds (policy
		// not yet active) delete it and retry to avoid a false pass.
		Eventually(func(g Gomega) {
			cronJob := sampleCronJob("escalation-attempt", "oran-o2ims-controller-manager")
			err := alarmsClient.Create(ctx, cronJob)
			if err == nil {
				_ = alarmsClient.Delete(ctx, cronJob)
			}
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring("default"))
		}, 30*time.Second, 500*time.Millisecond).Should(Succeed())
	})

	It("denies a CronJob that escalates via the deprecated serviceAccount field", func() {
		// A CronJob may set the deprecated spec...serviceAccount field instead of
		// serviceAccountName. The API server reconciles it into serviceAccountName
		// before admission, so the policy must still deny it. This guards against a
		// bypass of the serviceAccountName check.
		cronJob := sampleCronJob("deprecated-sa-escalation", "")
		cronJob.Spec.JobTemplate.Spec.Template.Spec.DeprecatedServiceAccount = "oran-o2ims-controller-manager"
		err := alarmsClient.Create(ctx, cronJob)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("default"))
	})

	It("denies an escalating CronJob created in a different namespace", func() {
		// The policy matches the alarms-server SA by name in any namespace, so an
		// attempt to sidestep it by creating the CronJob in another namespace is
		// still denied.
		cronJob := sampleCronJob("cross-ns-escalation", "oran-o2ims-controller-manager")
		cronJob.Namespace = otherNamespace
		err := alarmsClient.Create(ctx, cronJob)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("default"))
	})

	It("admits a CronJob whose pod ServiceAccount is unset", func() {
		cronJob := sampleCronJob("cleanup-unset-sa", "")
		Expect(alarmsClient.Create(ctx, cronJob)).To(Succeed())
	})

	It("admits a CronJob that explicitly runs as the default ServiceAccount", func() {
		cronJob := sampleCronJob("cleanup-default-sa", "default")
		Expect(alarmsClient.Create(ctx, cronJob)).To(Succeed())
	})
})

// cronJobRole mirrors the trimmed batch/cronjobs verbs granted to the
// alarms-server SA by config/rbac/role_alarms_server.yaml.
func cronJobRole(namespace string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "alarms-server-cronjobs", Namespace: namespace},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"batch"},
			Resources: []string{"cronjobs"},
			Verbs:     []string{"get", "create", "update"},
		}},
	}
}

// cronJobRoleBinding binds cronJobRole to the alarms-server ServiceAccount
// (which always lives in testNamespace) within the given namespace.
func cronJobRoleBinding(namespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "alarms-server-cronjobs", Namespace: namespace},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "alarms-server-cronjobs",
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "oran-o2ims-alarms-server",
			Namespace: testNamespace,
		}},
	}
}

// sampleCronJob builds a minimal CronJob with the given pod ServiceAccount name
// (empty leaves it unset).
func sampleCronJob(name, serviceAccountName string) *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 * * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							ServiceAccountName: serviceAccountName,
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{{
								Name:  "cleanup",
								Image: "registry.example.com/postgres:test",
							}},
						},
					},
				},
			},
		},
	}
}

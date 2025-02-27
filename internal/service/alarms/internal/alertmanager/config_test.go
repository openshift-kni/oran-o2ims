package alertmanager_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/alertmanager"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Alertmanager", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
		c      client.WithWatch

		// backup of the original getHubClient to restore later
		originalGetHubClient func() (client.WithWatch, error)
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)
		c = fake.NewClientBuilder().WithScheme(scheme).Build()
		ctx = context.Background()

		// Override the global getHubClient for testing.
		originalGetHubClient = alertmanager.GetHubClient
		alertmanager.GetHubClient = func() (client.WithWatch, error) {
			return c, nil
		}

		// Create the default secret with initial configuration.
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      alertmanager.ACMObsAMSecretName,
				Namespace: alertmanager.ACMObsAMNamespace,
			},
			Data: map[string][]byte{
				alertmanager.ACMObsAMSecretKey: []byte(
					`
global:
  resolve_timeout: 5m
receivers:
  - name: "null"
route:
  group_by:
  - namespace
  group_interval: 5m
  group_wait: 30s
  receiver: "null"
  repeat_interval: 12h
  routes:
    - match:
        alertname: Watchdog
      receiver: "null"
`),
			},
		}
		err := c.Create(ctx, secret)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Restore the original getHubClient.
		alertmanager.GetHubClient = originalGetHubClient
	})

	Describe("Setup", func() {
		It("verifies that the alertmanager.yaml key is populated", func() {
			err := alertmanager.Setup(ctx)
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			err = c.Get(ctx, client.ObjectKey{Namespace: alertmanager.ACMObsAMNamespace, Name: alertmanager.ACMObsAMSecretName}, secret)
			Expect(err).NotTo(HaveOccurred())

			// Expected configuration for comparison.
			mergedconfig := `
global:
  resolve_timeout: 5m
receivers:
  - name: oran_alarm_receiver
    webhook_configs:
      - http_config:
          authorization:
            credentials_file: /var/run/secrets/kubernetes.io/serviceaccount/token
            type: Bearer
          tls_config:
            ca_file: /var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt
        send_resolved: true
        url: https://alarms-server.oran-o2ims.svc.cluster.local:/internal/v1/caas-alerts/alertmanager
  - name: "null"
route:
  group_by: []
  group_interval: 5m
  group_wait: 30s
  receiver: "null"
  repeat_interval: 12h
  routes:
    - continue: true
      group_interval: 1m
      group_wait: 30s
      matchers:
        - alertname!~"Watchdog"
      receiver: oran_alarm_receiver
      repeat_interval: 4h
    - match:
        alertname: Watchdog
      receiver: "null"
`
			var config map[string]interface{}
			Expect(yaml.Unmarshal(secret.Data[alertmanager.ACMObsAMSecretKey], &config)).NotTo(HaveOccurred())

			var expectedConf map[string]interface{}
			Expect(yaml.Unmarshal([]byte(mergedconfig), &expectedConf)).NotTo(HaveOccurred())

			Expect(config).To(Equal(expectedConf))
		})

		It("returns an error if secret does not contain alertmanager.yaml key", func() {
			// Retrieve the secret and remove the alertmanager.yaml key.
			secret := &corev1.Secret{}
			err := c.Get(ctx, client.ObjectKey{Namespace: alertmanager.ACMObsAMNamespace, Name: alertmanager.ACMObsAMSecretName}, secret)
			Expect(err).NotTo(HaveOccurred())

			secret.Data = map[string][]byte{} // remove the key
			err = c.Update(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			err = alertmanager.Setup(ctx)
			Expect(err).To(HaveOccurred())
		})

		It("does not duplicate oran configuration when run multiple times", func() {
			// Run Setup twice.
			err := alertmanager.Setup(ctx)
			Expect(err).NotTo(HaveOccurred())

			err = alertmanager.Setup(ctx)
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			err = c.Get(ctx, client.ObjectKey{Namespace: alertmanager.ACMObsAMNamespace, Name: alertmanager.ACMObsAMSecretName}, secret)
			Expect(err).NotTo(HaveOccurred())

			var config map[string]interface{}
			Expect(yaml.Unmarshal(secret.Data[alertmanager.ACMObsAMSecretKey], &config)).NotTo(HaveOccurred())

			receivers, ok := config["receivers"].([]interface{})
			Expect(ok).To(BeTrue())

			count := 0
			for _, r := range receivers {
				rMap, ok := r.(map[string]interface{})
				if ok && rMap["name"] == alertmanager.OranReceiverName {
					count++
				}
			}
			Expect(count).To(Equal(1))
		})

		It("preserves existing non-oran configuration", func() {
			// Update the secret with an extra receiver and route.
			secret := &corev1.Secret{}
			err := c.Get(ctx, client.ObjectKey{Namespace: alertmanager.ACMObsAMNamespace, Name: alertmanager.ACMObsAMSecretName}, secret)
			Expect(err).NotTo(HaveOccurred())

			extraConfig := `
global:
  resolve_timeout: 5m
receivers:
  - name: extra_receiver
route:
  group_by:
  - namespace
  group_interval: 5m
  group_wait: 30s
  receiver: extra_receiver
  repeat_interval: 12h
  routes:
    - match:
        alertname: Watchdog
      receiver: extra_receiver
`
			secret.Data[alertmanager.ACMObsAMSecretKey] = []byte(extraConfig)
			err = c.Update(ctx, secret)
			Expect(err).NotTo(HaveOccurred())

			err = alertmanager.Setup(ctx)
			Expect(err).NotTo(HaveOccurred())

			updatedSecret := &corev1.Secret{}
			err = c.Get(ctx, client.ObjectKey{Namespace: alertmanager.ACMObsAMNamespace, Name: alertmanager.ACMObsAMSecretName}, updatedSecret)
			Expect(err).NotTo(HaveOccurred())

			var config map[string]interface{}
			Expect(yaml.Unmarshal(updatedSecret.Data[alertmanager.ACMObsAMSecretKey], &config)).NotTo(HaveOccurred())

			// Ensure the extra_receiver remains present.
			receivers, ok := config["receivers"].([]interface{})
			Expect(ok).To(BeTrue())

			foundExtra := false
			for _, r := range receivers {
				rMap, ok := r.(map[string]interface{})
				if ok && rMap["name"] == "extra_receiver" {
					foundExtra = true
					break
				}
			}
			Expect(foundExtra).To(BeTrue())
		})
	})

	Describe("addOranRouteToConfig", func() {
		It("returns an error for invalid YAML input", func() {
			invalidYAML := []byte("invalid: yaml: :::")
			_, err := alertmanager.AddOranRouteToConfig(invalidYAML)
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when configuration is empty", func() {
			emptyYAML := []byte("")
			_, err := alertmanager.AddOranRouteToConfig(emptyYAML)
			Expect(err).To(HaveOccurred())
		})
	})
})

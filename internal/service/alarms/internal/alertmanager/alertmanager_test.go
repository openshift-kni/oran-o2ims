package alertmanager

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

var _ = Describe("Alertmanager", func() {
	Describe("Setup", func() {
		var (
			ctx    context.Context
			scheme *runtime.Scheme
			c      client.Client
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			ctx = context.Background()
			c = fake.NewClientBuilder().WithScheme(scheme).Build()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					secretKey: []byte(""),
				},
			}

			err := c.Create(ctx, secret)
			Expect(err).NotTo(HaveOccurred())
		})

		It("verifies that the alertmanager.yaml key is populated", func() {
			err := Setup(ctx, c)
			Expect(err).ToNot(HaveOccurred())

			secret := &corev1.Secret{}
			err = c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
			Expect(err).NotTo(HaveOccurred())
			data := strings.Replace(string(alertManagerConfig), "{{ .url }}", utils.GetServiceURL(utils.InventoryAlarmServerName), 1)
			data = strings.Replace(data, "{{ .caFile }}", utils.DefaultServiceCAFile, 1)
			data = strings.Replace(data, "{{ .bearerTokenFile }}", utils.DefaultBackendTokenFile, 1)

			Expect(secret.Data[secretKey]).To(Equal([]byte(data)))
		})
	})
})

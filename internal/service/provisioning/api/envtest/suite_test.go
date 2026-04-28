/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package envtest

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8senvtest "sigs.k8s.io/controller-runtime/pkg/envtest"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
	provisioningapi "github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api"
	"github.com/openshift-kni/oran-o2ims/internal/service/provisioning/api/generated"
)

var (
	testEnv   *k8senvtest.Environment
	k8sClient client.Client
	ts        *httptest.Server
	baseURL   string
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestProvisioningApiEnvtest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provisioning API Envtest Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	handler := slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))

	scheme := runtime.NewScheme()
	Expect(provisioningv1alpha1.AddToScheme(scheme)).To(Succeed())

	testEnv = &k8senvtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())

	swagger, err := generated.GetSwagger()
	Expect(err).ToNot(HaveOccurred())

	server := provisioningapi.ProvisioningServer{
		HubClient: k8sClient,
	}

	strictHandler := generated.NewStrictHandlerWithOptions(&server, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  middleware.GetOranReqErrFunc(),
			ResponseErrorHandlerFunc: middleware.GetOranRespErrFunc(),
		},
	)

	logger := slog.New(slog.NewTextHandler(GinkgoWriter, &slog.HandlerOptions{Level: slog.LevelDebug}))
	filterAdapter, err := middleware.NewFilterAdapterFromSwagger(logger, swagger)
	Expect(err).ToNot(HaveOccurred())

	baseRouter := http.NewServeMux()
	opt := generated.StdHTTPServerOptions{
		BaseRouter: baseRouter,
		Middlewares: []generated.MiddlewareFunc{
			middleware.OpenAPIValidation(swagger),
			middleware.ResponseFilter(filterAdapter),
			middleware.LogDuration(),
		},
		ErrorHandlerFunc: middleware.GetOranReqErrFunc(),
	}
	generated.HandlerWithOptions(strictHandler, opt)

	mux := middleware.ChainHandlers(baseRouter,
		middleware.ErrorJsonifier(),
		middleware.TrailingSlashStripper(),
	)

	ts = httptest.NewServer(mux)
	baseURL = ts.URL
})

var _ = AfterSuite(func() {
	if ts != nil {
		ts.Close()
	}
	cancel()
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/util/cert"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/metrics"
)

var _ = Describe("WithClientVerification", func() {
	var request http.Request
	var tokenAuthenticator authenticator.Request
	var noopAuthenticator NoopAuthenticator
	var testCertificate, testStdCertificate string
	var testFingerprint string
	var logBuffer bytes.Buffer
	var origLogger *slog.Logger

	BeforeEach(func() {
		logBuffer.Reset()
		origLogger = slog.Default()
		slog.SetDefault(slog.New(logging.NewContextHandler(
			slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}), slog.LevelDebug)))
		pemBytes, _, err := cert.GenerateSelfSignedCertKey("localhost", nil, nil)
		Expect(err).ToNot(HaveOccurred())
		var certsBytes []byte
		for block, rest := pem.Decode(pemBytes); block != nil; block, rest = pem.Decode(rest) {
			certsBytes = append(certsBytes, block.Bytes...)
		}
		testCerts, err := x509.ParseCertificates(certsBytes)
		Expect(err).ToNot(HaveOccurred())
		fingerprint := sha256.Sum256(testCerts[0].Raw)
		testCertificate = base64.StdEncoding.EncodeToString(testCerts[0].Raw)
		testStdCertificate = ":" + testCertificate + ":"
		testFingerprint = strings.Trim(base64.URLEncoding.EncodeToString(fingerprint[:]), "=")
		noopAuthenticator = NoopAuthenticator{
			Response: &authenticator.Response{
				User: &user.DefaultInfo{
					Name:   "test",
					UID:    "1234",
					Groups: nil,
					Extra: map[string][]string{
						fingerprintKey: {testFingerprint},
					},
				},
			},
			Ok:    true,
			Error: nil,
		}
		tokenAuthenticator = WithClientVerification(&noopAuthenticator)
		request = http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Path: "/o2ims-infrastructureInventory/v1/resourcePools"},
			Header: http.Header{
				sslClientCertKey:  []string{testStdCertificate},
				sslClientChainKey: []string{testStdCertificate},
			},
		}
		ServiceName = "test-service"
		Expect(tokenAuthenticator).ToNot(BeNil())
	})

	AfterEach(func() {
		slog.SetDefault(origLogger)
	})

	It("authorizes a request with RFC9440 compliant headers", func() {
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(response.User.GetName()).To(Equal("test"))
		Expect(request.Header.Get(sslClientCertKey)).To(Equal(""))
		Expect(request.Header.Get(sslClientChainKey)).To(Equal(""))
	})

	It("rejects a request with RFC9440 headers without certificate", func() {
		request.Header.Del(sslClientCertKey)
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("a client certificate is required"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("authorizes a request that without a client binding", func() {
		delete(noopAuthenticator.Response.User.GetExtra(), fingerprintKey)
		tokenAuthenticator = WithClientVerification(&noopAuthenticator)
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(response.User.GetName()).To(Equal("test"))
	})

	It("rejects a request with RFC9440 headers with invalid certificate encoding", func() {
		request.Header.Set(sslClientCertKey, "invalid")
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err.Error()).To(ContainSubstring("error decoding client certificate"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("rejects a request with RFC9440 headers with invalid certificate encoding", func() {
		cert := []rune(testStdCertificate)
		cert[2] = 'B'
		request.Header.Set(sslClientCertKey, string(cert))
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("error parsing client certificate"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("authorizes a request with HAProxy compliant headers", func() {
		request.Header.Del(sslClientCertKey)
		request.Header.Del(sslClientChainKey)
		request.Header.Set(sslClientDERHeaderKey, testCertificate)
		request.Header.Set(sslClientVerifiedHeaderKey, "0")
		request.Header.Set(sslChainDERHeaderKey, "test")
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(response.User.GetName()).To(Equal("test"))
		Expect(request.Header.Get(sslClientDERHeaderKey)).To(Equal(""))
		Expect(request.Header.Get(sslClientVerifiedHeaderKey)).To(Equal(""))
		Expect(request.Header.Get(sslChainDERHeaderKey)).To(Equal(""))
	})

	It("rejects a request with HAProxy headers without valid certificate", func() {
		request.Header.Del(sslClientCertKey)
		request.Header.Del(sslClientChainKey)
		request.Header.Set(sslClientDERHeaderKey, testCertificate)
		request.Header.Set(sslClientVerifiedHeaderKey, "1")
		request.Header.Set(sslChainDERHeaderKey, "test")
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("rejects a request with HAProxy headers without certificate", func() {
		request.Header.Del(sslClientCertKey)
		request.Header.Del(sslClientChainKey)
		request.Header.Set(sslClientVerifiedHeaderKey, "0")
		request.Header.Set(sslChainDERHeaderKey, "test")
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("rejects a request with empty fingerprint value", func() {
		noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{""}
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("empty fingerprint value in token binding claim"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("reject a request with mismatched fingerprints", func() {
		noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{"other"}
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("client certificate fingerprint mismatch"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
		Expect(request.Header.Get(sslClientCertKey)).To(Equal(""))
		Expect(request.Header.Get(sslClientChainKey)).To(Equal(""))
	})

	It("reject a request with multiple fingerprints", func() {
		noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{"foo", "bar"}
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("unexpected number of fingerprint values"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
		Expect(request.Header.Get(sslClientCertKey)).To(Equal(""))
		Expect(request.Header.Get(sslClientChainKey)).To(Equal(""))
	})

	It("Logs container and clientIp on certificate verification failure", func() {
		request.RemoteAddr = "10.0.0.99:5555"
		ctx := logging.AppendCtx(request.Context(), slog.String("container", containerID))
		ctx = logging.AppendCtx(ctx, slog.String("clientIp", clientIP(&request)))
		request = *request.WithContext(ctx)
		noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{"other"}
		_, _, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(HaveOccurred())

		logOutput := logBuffer.String()
		Expect(logOutput).To(ContainSubstring(`"container"`))
		Expect(logOutput).To(ContainSubstring(`"clientIp":"10.0.0.99"`))
	})

	Context("metrics instrumentation", func() {
		BeforeEach(func() {
			metrics.AuthFailures.Reset()
		})

		It("increments certificate_binding counter on missing client certificate", func() {
			request.Header.Del(sslClientCertKey)
			_, _, _ = tokenAuthenticator.AuthenticateRequest(&request)
			Expect(getCounterValue(metrics.AuthFailures, "test-service", "certificate_binding", "GET", "/o2ims-infrastructureInventory/v1/resourcePools")).To(Equal(float64(1)))
		})

		It("increments certificate_binding counter on fingerprint mismatch", func() {
			noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{"other"}
			_, _, _ = tokenAuthenticator.AuthenticateRequest(&request)
			Expect(getCounterValue(metrics.AuthFailures, "test-service", "certificate_binding", "GET", "/o2ims-infrastructureInventory/v1/resourcePools")).To(Equal(float64(1)))
		})

		It("increments certificate_binding counter on empty fingerprint", func() {
			noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{""}
			_, _, _ = tokenAuthenticator.AuthenticateRequest(&request)
			Expect(getCounterValue(metrics.AuthFailures, "test-service", "certificate_binding", "GET", "/o2ims-infrastructureInventory/v1/resourcePools")).To(Equal(float64(1)))
		})

		It("increments certificate_binding counter on multiple fingerprints", func() {
			noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{"foo", "bar"}
			_, _, _ = tokenAuthenticator.AuthenticateRequest(&request)
			Expect(getCounterValue(metrics.AuthFailures, "test-service", "certificate_binding", "GET", "/o2ims-infrastructureInventory/v1/resourcePools")).To(Equal(float64(1)))
		})

		It("does not increment counter on success", func() {
			_, _, _ = tokenAuthenticator.AuthenticateRequest(&request)
			Expect(getCounterValue(metrics.AuthFailures, "test-service", "certificate_binding", "GET", "/o2ims-infrastructureInventory/v1/resourcePools")).To(Equal(float64(0)))
		})
	})
})

func getCounterValue(cv *prometheus.CounterVec, labels ...string) float64 {
	counter, err := cv.GetMetricWithLabelValues(labels...)
	if err != nil {
		return 0
	}
	m := &dto.Metric{}
	if err := counter.Write(m); err != nil {
		return 0
	}
	return m.GetCounter().GetValue()
}

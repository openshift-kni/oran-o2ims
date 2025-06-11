/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/util/cert"
)

var _ = Describe("WithClientVerification", func() {
	var request http.Request
	var tokenAuthenticator authenticator.Request
	var noopAuthenticator NoopAuthenticator
	var testCertificate, testStdCertificate string
	var testFingerprint string

	BeforeEach(func() {
		pemBytes, _, err := cert.GenerateSelfSignedCertKey("localhost", nil, nil)
		Expect(err).To(BeNil())
		var certsBytes []byte
		for block, rest := pem.Decode(pemBytes); block != nil; block, rest = pem.Decode(rest) {
			certsBytes = append(certsBytes, block.Bytes...)
		}
		testCerts, err := x509.ParseCertificates(certsBytes)
		Expect(err).To(BeNil())
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
		request = http.Request{Header: http.Header{
			sslClientCertKey:  []string{testStdCertificate},
			sslClientChainKey: []string{testStdCertificate},
		}}
		Expect(tokenAuthenticator).ToNot(BeNil())
	})

	It("authorizes a request with RFC9440 compliant headers", func() {
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(BeNil())
		Expect(ok).To(BeTrue())
		Expect(response.User.GetName()).To(Equal("test"))
		Expect(request.Header.Get(sslClientCertKey)).To(Equal(""))
		Expect(request.Header.Get(sslClientChainKey)).To(Equal(""))
	})

	It("rejects a request with RFC9440 headers without certificate", func() {
		request.Header.Del(sslClientCertKey)
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(Equal("a client certificate is required"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("authorizes a request that without a client binding", func() {
		delete(noopAuthenticator.Response.User.GetExtra(), fingerprintKey)
		tokenAuthenticator = WithClientVerification(&noopAuthenticator)
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).To(BeNil())
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
		Expect(err).ToNot(BeNil())
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
		Expect(err).To(BeNil())
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
		Expect(err).ToNot(BeNil())
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("rejects a request with HAProxy headers without certificate", func() {
		request.Header.Del(sslClientCertKey)
		request.Header.Del(sslClientChainKey)
		request.Header.Set(sslClientVerifiedHeaderKey, "0")
		request.Header.Set(sslChainDERHeaderKey, "test")
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).ToNot(BeNil())
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
	})

	It("reject a request with mismatched fingerprints", func() {
		noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{"other"}
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(Equal("client certificate fingerprint mismatch"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
		Expect(request.Header.Get(sslClientCertKey)).To(Equal(""))
		Expect(request.Header.Get(sslClientChainKey)).To(Equal(""))
	})

	It("reject a request with multiple fingerprints", func() {
		noopAuthenticator.Response.User.GetExtra()[fingerprintKey] = []string{"foo", "bar"}
		response, ok, err := tokenAuthenticator.AuthenticateRequest(&request)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(Equal("unexpected number of fingerprint values"))
		Expect(ok).To(BeFalse())
		Expect(response).To(BeNil())
		Expect(request.Header.Get(sslClientCertKey)).To(Equal(""))
		Expect(request.Header.Get(sslClientChainKey)).To(Equal(""))
	})

})

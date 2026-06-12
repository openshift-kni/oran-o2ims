// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package notifier_test

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

var _ = Describe("blockCrossHostRedirects", func() {
	It("should follow same-host redirects", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirect" {
				http.Redirect(w, r, "/target", http.StatusFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := server.Client()
		notifier.BlockCrossHostRedirects(client)

		resp, err := client.Get(server.URL + "/redirect")
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("should block cross-host redirects", func() {
		externalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer externalServer.Close()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, externalServer.URL+"/steal", http.StatusFound)
		}))
		defer server.Close()

		client := server.Client()
		notifier.BlockCrossHostRedirects(client)

		_, err := client.Get(server.URL + "/start")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cross-host redirect blocked"))
	})
})

package api_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bytes"
	"crypto/tls"
	. "github.com/openshift-kni/oran-o2ims/internal/testing"
	"io"
	"net/http"
	// "github.com/openshift-kni/oran-o2ims/test/api"
)

var _ = Describe("Deployment Manager Server API testing", func() {
	var client *http.Client

	BeforeEach(func() {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: tr}
	})

	Context("When getting Deployment managers description", func() {
		It("should return OK in the response and json response should match json", func() {
			requestBody := []byte(``)
			request, _ := http.NewRequest("GET",
        "https://" + testHost + resUrl + "deploymentManagers",
        bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")
			By("Executing http petition")
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			By("Checking JSON match")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJQ(`.name`, "OpenShift O-Cloud"))
		})
	})
})

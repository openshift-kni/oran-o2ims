package api_test

import (
	"bytes"
	"crypto/tls"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/openshift-kni/oran-o2ims/internal/testing"
	"io"
	"net/http"
	// "github.com/openshift-kni/oran-o2ims/test/api"
)

var _ = Describe("Metadata Server API testing", func() {
	var client *http.Client
	BeforeEach(func() {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: tr}
	})
	Context("When getting infrastructure Inventory API version", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(``)
			request, _ := http.NewRequest("GET",
        "https://"+ testHost + metaUrl + "api_versions",
        bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")
			By("Executing https petition")
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			By("Checking response JSON matches condition")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJQ(`version`, version))
		})
	})
	Context("When getting infrastructure Inventory description", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(``)
			request, _ := http.NewRequest("GET",
        "https://" + testHost + metaUrl + "v1",
        bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")
			By("Executing https petition")
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			By("Checking response JSON matches condition")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJQ(`.name`, "OpenShift O-Cloud"))
		})
	})
})

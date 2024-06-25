package api_test

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	// "github.com/openshift-kni/oran-o2ims/test/api"
	. "github.com/openshift-kni/oran-o2ims/internal/testing"
)

var _ = Describe("Resources Server API testing", func() {
	var client *http.Client

	BeforeEach(func() {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: tr}
	})

	Context("When getting Resource Type list", func() {
		It("should return OK in the response and json response should match condition", func() {
			requestBody := []byte(``)
			request, _ := http.NewRequest("GET",
        "https://" + testHost + resUrl + "resourceTypes",
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
	Context("When getting Resource Pools", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(``)
			request, _ := http.NewRequest("GET",
        "https://" + testHost + resUrl + "resourcePools",
        bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")
			By("Executing https petition")
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			By("Checking response JSON is equal to expected JSON")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJQ(`.name`, resPool))
		})
	})
	Context("When getting Resource List from a defined pool", func() {
		It("should return OK in the response and json response should match reference json", func() {
			requestBody := []byte(``)
			request, _ := http.NewRequest("GET",
        "https://" + testHost + resUrl + "resourcePools/" + resPool + "/resources",
        bytes.NewBuffer([]byte(requestBody)))
			request.Header.Set("Content-Type", "application/json")
			By("Executing https petition")
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			By("Checking OK status response")
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			By("Checking response JSON matches condition")
			responseBody, _ := io.ReadAll(response.Body)
			Expect(responseBody).To(MatchJQ(`.resourcePoolID`, resPool))
		})
	})
})

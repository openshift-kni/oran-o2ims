package api_test

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)
var testHost string
const version = "1.0.0"
const resPool = "local-cluster"
const metaUrl = "/o2ims-infrastructureInventory/"
const resUrl = "/o2ims-infrastructureInventory/v1/"
const alarUrl = "/o2ims-infrastructureMonitoring/v1/"
var _ = BeforeSuite(func() {
  testHost := os.Getenv("TEST_HOST")
  Expect(testHost).NotTo(BeZero(), "Please make sure TEST_HOST is correctly set")
    if testHost == "" {
		Skip("API tests were skipped because environment variable 'TEST_HOST' isn't set")
	}
})

func TestApi(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Api Suite")
}

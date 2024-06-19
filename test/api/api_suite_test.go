package api_test

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = BeforeSuite(func() {
	Expect(os.Getenv("TEST_HOST")).NotTo(BeZero(), "Please make sure TEST_HOST is set correctly.")
	if os.Getenv("TEST_HOST") == "" {
		Skip("API tests were skipped because environment variable 'TEST_HOST' isn't set")
	}
})

func TestApi(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Api Suite")
}

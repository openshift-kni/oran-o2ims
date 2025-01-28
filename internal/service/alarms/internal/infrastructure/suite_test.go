package infrastructure

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRepo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alarms Infrastructure Suite")
}

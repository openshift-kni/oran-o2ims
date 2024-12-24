package dictionary_definition_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDictionary(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dictionary Definition Suite")
}

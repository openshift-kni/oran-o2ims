package v1alpha1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestProvisioningApiSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provisioning API Suite")
}

var s = scheme.Scheme

var _ = BeforeSuite(func() {
	s.AddKnownTypes(GroupVersion, &ProvisioningRequest{}, &ClusterTemplate{}, &ClusterTemplateList{})
})

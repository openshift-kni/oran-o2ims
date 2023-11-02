/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package authentication

import (
	"log/slog"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	. "github.com/openshift-kni/oran-o2ims/internal/testing"
)

func TestAuthentication(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Authentication")
}

// Logger used for tests:
var logger *slog.Logger

// JSON web key set used for tests:
var keysBytes []byte
var keysFile string

var _ = BeforeSuite(func() {
	var err error

	// Create a temporary file containing the JSON web key set:
	keysBytes = DefaultJWKS()
	keysFD, err := os.CreateTemp("", "jwks-*.json")
	Expect(err).ToNot(HaveOccurred())
	_, err = keysFD.Write(keysBytes)
	Expect(err).ToNot(HaveOccurred())
	err = keysFD.Close()
	Expect(err).ToNot(HaveOccurred())
	keysFile = keysFD.Name()

	// Create a logger that writes to the Ginkgo writer, so that the log messages will be
	// attached to the output of the right test:
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	logger = slog.New(handler)
})

var _ = AfterSuite(func() {
	// Delete the temporary files:
	err := os.Remove(keysFile)
	Expect(err).ToNot(HaveOccurred())
})

/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package network

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
)

var _ = Describe("Listener", func() {
	var tmp string

	BeforeEach(func() {
		// In order to avoid TCP port conflicts these tests will use only Unix sockets
		// created in this temporary directory:
		var err error
		tmp, err = os.MkdirTemp("", "*.sockets")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err := os.RemoveAll(tmp)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Can't be created without a logger", func() {
		address := filepath.Join(tmp, "my.socket")
		listener, err := NewListener().
			SetNetwork("unix").
			SetAddress(address).
			Build()
		Expect(err).To(HaveOccurred())
		Expect(listener).To(BeNil())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("logger"))
		Expect(msg).To(ContainSubstring("mandatory"))
	})

	It("Can't be created without an address", func() {
		listener, err := NewListener().
			SetLogger(logger).
			SetNetwork("unix").
			Build()
		Expect(err).To(HaveOccurred())
		Expect(listener).To(BeNil())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("address"))
		Expect(msg).To(ContainSubstring("mandatory"))
	})

	It("Can't be created with an incorrect address", func() {
		listener, err := NewListener().
			SetLogger(logger).
			SetAddress("junk").
			Build()
		Expect(err).To(HaveOccurred())
		Expect(listener).To(BeNil())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("junk"))
	})

	It("Uses the given address", func() {
		address := filepath.Join(tmp, "my.socket")
		listener, err := NewListener().
			SetLogger(logger).
			SetNetwork("unix").
			SetAddress(address).
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(listener).ToNot(BeNil())
		Expect(listener.Addr().String()).To(Equal(address))
	})

	It("Honors the address flag", func() {
		// Prepare the flags:
		address := filepath.Join(tmp, "my.socket")
		flags := pflag.NewFlagSet("", pflag.ContinueOnError)
		AddListenerFlags(flags, "my", "localhost:80")
		err := flags.Parse([]string{
			"--my-listener-address", address,
		})
		Expect(err).ToNot(HaveOccurred())

		// Create the listener:
		listener, err := NewListener().
			SetLogger(logger).
			SetNetwork("unix").
			SetFlags(flags, "my").
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(listener).ToNot(BeNil())
		Expect(listener.Addr().String()).To(Equal(address))
	})

	It("Ignores flags for other listeners", func() {
		// Prepare the flags:
		myAddress := filepath.Join(tmp, "my.socket")
		yourAddress := filepath.Join(tmp, "your.socket")
		flags := pflag.NewFlagSet("", pflag.ContinueOnError)
		AddListenerFlags(flags, "my", "localhost:80")
		AddListenerFlags(flags, "your", "localhost:81")
		err := flags.Parse([]string{
			"--my-listener-address", myAddress,
			"--your-listener-address", yourAddress,
		})
		Expect(err).ToNot(HaveOccurred())

		// Create the listener:
		listener, err := NewListener().
			SetLogger(logger).
			SetNetwork("unix").
			SetFlags(flags, "my").
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(listener).ToNot(BeNil())
		Expect(listener.Addr().String()).To(Equal(myAddress))
	})
})

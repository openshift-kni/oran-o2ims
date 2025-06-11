/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package internal

import (
	"bytes"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
)

var _ = Describe("Tool", func() {
	var logger *slog.Logger

	BeforeEach(func() {
		var err error

		// Create a logger:
		logger, err = logging.NewLogger().
			SetWriter(GinkgoWriter).
			SetLevel("debug").
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Can't be created without at least one argument", func() {
		tool, err := NewTool().
			SetLogger(logger).
			SetIn(&bytes.Buffer{}).
			SetOut(io.Discard).
			SetErr(io.Discard).
			Build()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("binary"))
		Expect(msg).To(ContainSubstring("required"))
		Expect(tool).To(BeNil())
	})

	It("Can't be created standard input stream", func() {
		tool, err := NewTool().
			SetLogger(logger).
			AddArgs("o2ims").
			SetOut(io.Discard).
			SetErr(io.Discard).
			Build()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("input"))
		Expect(msg).To(ContainSubstring("mandatory"))
		Expect(tool).To(BeNil())
	})

	It("Can't be created standard output stream", func() {
		tool, err := NewTool().
			SetLogger(logger).
			AddArgs("o2ims").
			SetIn(&bytes.Buffer{}).
			SetErr(io.Discard).
			Build()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("output"))
		Expect(msg).To(ContainSubstring("mandatory"))
		Expect(tool).To(BeNil())
	})

	It("Can't be created standard error stream", func() {
		tool, err := NewTool().
			SetLogger(logger).
			AddArgs("o2ims").
			SetIn(&bytes.Buffer{}).
			SetOut(io.Discard).
			Build()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("error"))
		Expect(msg).To(ContainSubstring("mandatory"))
		Expect(tool).To(BeNil())
	})
})

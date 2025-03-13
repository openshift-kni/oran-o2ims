/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package internal

import (
	"bytes"
	"context"
	"io"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/logging"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = Describe("Context", func() {
	var logger *slog.Logger

	BeforeEach(func() {
		var err error
		logger, err = logging.NewLogger().
			SetWriter(GinkgoWriter).
			SetLevel("debug").
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Extracts tool from the context if previously added", func() {
		original, err := NewTool().
			SetLogger(logger).
			AddArgs("o2ims").
			SetIn(&bytes.Buffer{}).
			SetOut(io.Discard).
			SetErr(io.Discard).
			Build()
		Expect(err).ToNot(HaveOccurred())
		ctx := ToolIntoContext(context.Background(), original)
		extracted := ToolFromContext(ctx)
		Expect(extracted).To(BeIdenticalTo(original))
	})

	It("Panics if tool wasn't added to the context", func() {
		ctx := context.Background()
		Expect(func() {
			ToolFromContext(ctx)
		}).To(Panic())
	})

	It("Extracts logger from the context if previously added", func() {
		ctx := LoggerIntoContext(context.Background(), logger)
		extracted := LoggerFromContext(ctx)
		Expect(extracted).To(BeIdenticalTo(logger))
	})

	It("Panics if logger wasn't added to the context", func() {
		ctx := context.Background()
		Expect(func() {
			LoggerFromContext(ctx)
		}).To(Panic())
	})
})

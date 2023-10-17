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

package internal

import (
	"bytes"
	"context"
	"io"
	"log/slog"

	"github.com/jhernand/o2ims/internal/logging"
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

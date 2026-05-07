/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package internal

import (
	"context"
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

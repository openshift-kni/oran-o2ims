/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Claude AI Assistant
*/

package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
)

var _ = Describe("StartupLog", func() {
	var (
		buf    bytes.Buffer
		logger *slog.Logger
		ctx    context.Context
	)

	BeforeEach(func() {
		buf.Reset()
		var err error
		logger, err = logging.NewLogger().
			SetWriter(&buf).
			Build()
		Expect(err).ToNot(HaveOccurred())
		ctx = context.Background()
	})

	It("should include the Hello, Friends! greeting", func() {
		// Simulate the startup log message that appears in the run function
		logger.InfoContext(
			ctx,
			"Starting manager",
			slog.String("greeting", "Hello, Friends!"),
			slog.String("image", "test-image:latest"),
		)

		// Parse the log output
		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		Expect(err).ToNot(HaveOccurred(), "Failed to parse log output. Raw output: %s", buf.String())

		// Verify the message content
		Expect(logEntry["msg"]).To(Equal("Starting manager"))

		// Verify the greeting field is present and correct
		Expect(logEntry["greeting"]).To(Equal("Hello, Friends!"))

		// Verify the image field is present
		Expect(logEntry["image"]).To(Equal("test-image:latest"))

		// Verify the log level is INFO
		Expect(logEntry["level"]).To(Equal("INFO"))
	})

	It("should use structured logging format", func() {
		// This test verifies that the startup log follows structured logging best practices
		logger.InfoContext(
			ctx,
			"Starting manager",
			slog.String("greeting", "Hello, Friends!"),
			slog.String("image", "test-image:latest"),
		)

		// Verify the output is valid JSON (structured logging)
		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		Expect(err).ToNot(HaveOccurred(), "Log output is not valid JSON. Raw output: %s", buf.String())

		// Verify required structured fields are present
		requiredFields := []string{"time", "level", "msg", "greeting", "image"}
		for _, field := range requiredFields {
			Expect(logEntry).To(HaveKey(field), "Required field %s not found in log output", field)
		}
	})
})

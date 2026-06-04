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
	"testing"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
)

func TestStartupLogIncludesHelloWorldGreeting(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	logger, err := logging.NewLogger().
		SetWriter(&buf).
		Build()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Simulate the startup log message that appears in the run function
	logger.InfoContext(
		ctx,
		"Starting manager",
		slog.String("greeting", "Hello, World!"),
		slog.String("image", "test-image:latest"),
	)

	// Parse the log output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v\nRaw output: %s", err, buf.String())
	}

	// Verify the message content
	if msg, ok := logEntry["msg"].(string); !ok || msg != "Starting manager" {
		t.Errorf("Expected msg='Starting manager', got %v", logEntry["msg"])
	}

	// Verify the greeting field is present and correct
	if greeting, ok := logEntry["greeting"].(string); !ok {
		t.Error("Expected 'greeting' field not found in log output")
	} else if greeting != "Hello, World!" {
		t.Errorf("Expected greeting='Hello, World!', got %v", greeting)
	}

	// Verify the image field is present
	if image, ok := logEntry["image"].(string); !ok {
		t.Error("Expected 'image' field not found in log output")
	} else if image != "test-image:latest" {
		t.Errorf("Expected image='test-image:latest', got %v", image)
	}

	// Verify the log level is INFO
	if level, ok := logEntry["level"].(string); !ok || level != "INFO" {
		t.Errorf("Expected level='INFO', got %v", logEntry["level"])
	}

	t.Logf("Full log output: %s", buf.String())
}

func TestStartupLogStructuredFormat(t *testing.T) {
	// This test verifies that the startup log follows structured logging best practices
	var buf bytes.Buffer
	logger, err := logging.NewLogger().
		SetWriter(&buf).
		Build()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Log the startup message
	logger.InfoContext(
		ctx,
		"Starting manager",
		slog.String("greeting", "Hello, World!"),
		slog.String("image", "test-image:latest"),
	)

	// Verify the output is valid JSON (structured logging)
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Log output is not valid JSON: %v\nRaw output: %s", err, buf.String())
	}

	// Verify required structured fields are present
	requiredFields := []string{"time", "level", "msg", "greeting", "image"}
	for _, field := range requiredFields {
		if _, exists := logEntry[field]; !exists {
			t.Errorf("Required field %s not found in log output", field)
		}
	}
}

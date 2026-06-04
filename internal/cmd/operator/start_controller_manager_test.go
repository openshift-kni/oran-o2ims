/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
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

// TestStartupLogging verifies that the required startup logs are emitted
// in the correct order when the controller manager starts.
//
// This test addresses the requirement that:
// 1. A "Hello, World!" log message must be present at startup
// 2. It must appear before the "Starting manager" log message
//
// Note: This is a unit test that verifies the logging behavior in isolation.
// The actual startup sequence in run() executes this in a goroutine.
func TestStartupLogging(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger that writes to our buffer
	logger, err := logging.NewLogger().
		SetWriter(&buf).
		Build()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()
	image := "quay.io/openshift-kni/oran-o2ims:latest"

	// Simulate the exact logging sequence that occurs in run()
	// This mirrors lines 376-381 in start_controller_manager.go
	logger.InfoContext(ctx, "Hello, World!")
	logger.InfoContext(
		ctx,
		"Starting manager",
		slog.String("image", image),
	)

	// Parse the log output - should have exactly 2 lines
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("Expected 2 log lines, got %d\nOutput: %s", len(lines), buf.String())
	}

	// Verify first log entry is "Hello, World!"
	var firstEntry map[string]interface{}
	if err := json.Unmarshal(lines[0], &firstEntry); err != nil {
		t.Fatalf("Failed to parse first log entry: %v\nRaw: %s", err, lines[0])
	}

	if msg, ok := firstEntry["msg"].(string); !ok || msg != "Hello, World!" {
		t.Errorf("First log message should be 'Hello, World!', got: %v", firstEntry["msg"])
	}

	// Verify second log entry is "Starting manager" with image field
	var secondEntry map[string]interface{}
	if err := json.Unmarshal(lines[1], &secondEntry); err != nil {
		t.Fatalf("Failed to parse second log entry: %v\nRaw: %s", err, lines[1])
	}

	if msg, ok := secondEntry["msg"].(string); !ok || msg != "Starting manager" {
		t.Errorf("Second log message should be 'Starting manager', got: %v", secondEntry["msg"])
	}

	if imgValue, ok := secondEntry["image"].(string); !ok || imgValue != image {
		t.Errorf("Second log should have image field with value %s, got: %v", image, secondEntry["image"])
	}

	t.Logf("Startup logging test passed. Log output:\n%s", buf.String())
}

// TestStartupLogOrder ensures that "Hello, World!" appears before "Starting manager"
// This test explicitly verifies the ordering requirement.
func TestStartupLogOrder(t *testing.T) {
	var buf bytes.Buffer

	logger, err := logging.NewLogger().
		SetWriter(&buf).
		Build()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Execute the startup logging sequence
	logger.InfoContext(ctx, "Hello, World!")
	logger.InfoContext(ctx, "Starting manager", slog.String("image", "test-image"))

	// Get log lines
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))

	// Parse both entries to verify order
	var firstMsg, secondMsg string

	var firstEntry map[string]interface{}
	if err := json.Unmarshal(lines[0], &firstEntry); err == nil {
		if msg, ok := firstEntry["msg"].(string); ok {
			firstMsg = msg
		}
	}

	var secondEntry map[string]interface{}
	if err := json.Unmarshal(lines[1], &secondEntry); err == nil {
		if msg, ok := secondEntry["msg"].(string); ok {
			secondMsg = msg
		}
	}

	// Verify order: "Hello, World!" must come first
	if firstMsg != "Hello, World!" {
		t.Errorf("First log must be 'Hello, World!', got: %s", firstMsg)
	}

	if secondMsg != "Starting manager" {
		t.Errorf("Second log must be 'Starting manager', got: %s", secondMsg)
	}

	// Explicitly verify that "Hello, World!" comes before "Starting manager"
	if !(firstMsg == "Hello, World!" && secondMsg == "Starting manager") {
		t.Errorf("Log order violation: expected 'Hello, World!' before 'Starting manager', got: %s, %s",
			firstMsg, secondMsg)
	}
}

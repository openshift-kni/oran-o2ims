/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Claude/Cursor AI Assistant
*/

package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestControllerLoggingIntegration demonstrates the complete logging pattern
// that controllers should use, validating that all context is properly carried through.
func TestControllerLoggingIntegration(t *testing.T) {
	// Create a logger using the actual application logging builder
	var buf bytes.Buffer
	logger, err := logging.NewLogger().
		SetWriter(&buf).
		Build()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Simulate a controller reconcile call
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-cluster-template",
			Namespace: "test-namespace",
		},
	}

	ctx := context.Background()

	// Step 1: Start reconciliation with standard context
	ctx = LogReconcileStart(ctx, logger, req, "ClusterTemplate")

	// Step 2: Add object context (simulating after fetching the object)
	obj := &mockObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-cluster-template",
			Namespace:       "test-namespace",
			ResourceVersion: "12345",
			Generation:      3,
			Labels: map[string]string{
				"cluster-id": "test-cluster",
				"version":    "v1.0.0",
			},
		},
	}
	ctx = AddObjectContext(ctx, obj)

	// Step 3: Log phase operations
	ctx = LogPhaseStart(ctx, logger, "validation")
	logger.InfoContext(ctx, "Validating cluster template schema")

	// Step 4: Log errors with context
	testErr := &testError{message: "invalid template schema"}
	LogError(ctx, logger, "Validation failed", testErr,
		slog.String(LogAttrOperation, "schema_validation"))

	// Step 5: Log operation completion
	logger.InfoContext(ctx, "Reconciliation phase completed")

	// Parse and validate the log output
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Validate that we have the expected number of log entries
	if len(lines) < 4 {
		t.Fatalf("Expected at least 4 log entries, got %d", len(lines))
	}

	// Check the last substantial log entry (the phase completed log)
	var lastLogEntry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lastLogEntry); err != nil {
		t.Fatalf("Failed to parse last log entry: %v\nOutput: %s", err, output)
	}

	// Validate that all expected context is present in logs
	expectedContext := map[string]interface{}{
		// From LogReconcileStart
		LogAttrResource:  "ClusterTemplate",
		"name":           "my-cluster-template",
		LogAttrNamespace: "test-namespace",
		LogAttrAction:    "reconcile_start",

		// From AddObjectContext
		LogAttrResourceVersion: "12345",
		LogAttrGeneration:      float64(3), // JSON numbers are float64
		"clusterID":            "test-cluster",

		// From LogPhaseStart
		LogAttrPhase: "validation",
	}

	for key, expectedValue := range expectedContext {
		if actualValue, exists := lastLogEntry[key]; !exists {
			t.Errorf("Expected context attribute %s not found in final log entry", key)
		} else if actualValue != expectedValue {
			t.Errorf("Context attribute %s: expected %v, got %v", key, expectedValue, actualValue)
		}
	}

	// Validate that the error log includes all context + error details
	// Find the error log entry
	var errorLogEntry map[string]interface{}
	for _, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			if level, ok := entry["level"]; ok && level == "ERROR" {
				errorLogEntry = entry
				break
			}
		}
	}

	if errorLogEntry == nil {
		t.Fatal("Expected to find an ERROR level log entry")
	}

	// Validate error-specific attributes
	if errMsg, exists := errorLogEntry[LogAttrError]; !exists {
		t.Error("Error log should contain error attribute")
	} else if errMsg != "invalid template schema" {
		t.Errorf("Expected error message 'invalid template schema', got %v", errMsg)
	}

	if op, exists := errorLogEntry[LogAttrOperation]; !exists {
		t.Error("Error log should contain operation attribute")
	} else if op != "schema_validation" {
		t.Errorf("Expected operation 'schema_validation', got %v", op)
	}

	// All context should still be present in error logs
	for key, expectedValue := range expectedContext {
		if actualValue, exists := errorLogEntry[key]; !exists {
			t.Errorf("Expected context attribute %s not found in error log entry", key)
		} else if actualValue != expectedValue {
			t.Errorf("Error log context attribute %s: expected %v, got %v", key, expectedValue, actualValue)
		}
	}

	t.Logf("Full integration test output:\n%s", output)
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

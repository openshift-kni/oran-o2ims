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
	"testing"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mockObject implements client.Object for testing
type mockObject struct {
	metav1.ObjectMeta
}

func (m *mockObject) GetObjectKind() schema.ObjectKind { return &mockObjectKind{} }
func (m *mockObject) DeepCopyObject() runtime.Object   { return m }

type mockObjectKind struct{}

func (m *mockObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {}
func (m *mockObjectKind) GroupVersionKind() schema.GroupVersionKind       { return schema.GroupVersionKind{} }

func TestLogReconcileStart(t *testing.T) {
	// Test with the actual logging builder to ensure context attributes work
	var buf bytes.Buffer
	logger, err := logging.NewLogger().
		SetWriter(&buf).
		Build()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-resource",
			Namespace: "test-namespace",
		},
	}

	ctx := context.Background()
	ctx = LogReconcileStart(ctx, logger, req, "TestResource")

	// Verify the context is returned and not nil
	if ctx == nil {
		t.Fatal("Context should not be nil")
	}

	// Log a message to test that context attributes are included
	logger.InfoContext(ctx, "Test message with context")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("Expected two log entries, got %d: %s", len(lines), buf.String())
	}

	// Check that our context attributes are present
	expectedAttrs := map[string]interface{}{
		LogAttrResource:     "TestResource",
		LogAttrResourceName: "test-resource",
		LogAttrNamespace:    "test-namespace",
		LogAttrAction:       "reconcile_start",
	}

	// The first entry verifies the attributes are set before the start log;
	// the second verifies they remain in the returned context for later logs.
	for i, line := range lines {
		var logEntry map[string]interface{}
		if err := json.Unmarshal(line, &logEntry); err != nil {
			t.Fatalf("Failed to parse log entry %d: %v", i, err)
		}
		for key, expectedValue := range expectedAttrs {
			if actualValue, exists := logEntry[key]; !exists {
				t.Errorf("Expected attribute %s not found in log entry %d", key, i)
			} else if actualValue != expectedValue {
				t.Errorf("Expected %s=%v in log entry %d, got %v", key, expectedValue, i, actualValue)
			}
		}
	}

	t.Logf("Full log output: %s", buf.String())
}

func TestAddObjectContext(t *testing.T) {
	obj := &mockObject{
		ObjectMeta: metav1.ObjectMeta{
			ResourceVersion: "12345",
			Generation:      3,
			Labels: map[string]string{
				"cluster-id": "test-cluster",
			},
		},
	}

	ctx := context.Background()
	ctx = AddObjectContext(ctx, obj)

	// Verify the context is returned and not nil
	if ctx == nil {
		t.Fatal("Context should not be nil")
	}

	// Basic test to ensure the function doesn't panic and returns a context
	if ctx == context.Background() {
		t.Error("Context should be modified, not the same as background context")
	}
}

func TestLogError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{}))

	ctx := context.Background()
	testErr := &CustomError{message: "test error"}

	LogError(ctx, logger, "Test error occurred", testErr, slog.String("additional", "info"))

	// Parse the log output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log output: %v", err)
	}

	// Check that error is logged correctly
	if level, exists := logEntry["level"]; !exists || level != "ERROR" {
		t.Errorf("Expected log level ERROR, got %v", level)
	}

	if errMsg, exists := logEntry[LogAttrError]; !exists || errMsg != "test error" {
		t.Errorf("Expected error message 'test error', got %v", errMsg)
	}

	if additional, exists := logEntry["additional"]; !exists || additional != "info" {
		t.Errorf("Expected additional attribute 'info', got %v", additional)
	}
}

// CustomError for testing
type CustomError struct {
	message string
}

func (e *CustomError) Error() string {
	return e.message
}

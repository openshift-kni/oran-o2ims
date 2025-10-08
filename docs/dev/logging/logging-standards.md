# O-Cloud Manager Structured Logging Standards

<!--
SPDX-FileCopyrightText: Red Hat
SPDX-License-Identifier: Apache-2.0
Generated-By: Claude/Cursor AI Assistant
-->

## Overview

This document defines the structured logging standards for the O-Cloud Manager project. Our logging system provides comprehensive observability across the entire O-Cloud Manager stack, from infrastructure deployment to cluster provisioning to hardware management.

## Table of Contents

1. [Logging Architecture](#logging-architecture)
2. [Standard Patterns](#standard-patterns)
3. [Context Management](#context-management)
4. [Controller Logging](#controller-logging)
5. [Error Handling](#error-handling)
6. [Performance Tracking](#performance-tracking)
7. [Migration Guide](#migration-guide)
8. [Best Practices](#best-practices)
9. [Troubleshooting](#troubleshooting)

## Logging Architecture

### Core Components

```text
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Application   │────│ LoggerBuilder   │────│ slog.Logger     │
│   Context       │    │                 │    │ (JSON Output)   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│ Context         │────│LoggingContext   │────│ Structured      │
│ Attributes      │    │ Handler         │    │ JSON Logs       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### Key Components

- **LoggerBuilder** (`internal/logging/logger.go`): Creates properly configured JSON loggers
- **LoggingContextHandler** (`internal/logging/logging.go`): Extracts attributes from context
- **Controller Utilities** (`internal/controllers/utils/logging.go`): Standard logging helpers
- **Context Propagation**: Carries attributes through function calls

## Standard Patterns

### 1. Controller Reconciliation Pattern

Every controller should follow this standard pattern:

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    
    // Add standard reconciliation context
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "MyResource")
    
    defer func() {
        duration := time.Since(startTime)
        if err != nil {
            r.Logger.ErrorContext(ctx, "Reconciliation failed",
                slog.Duration("duration", duration),
                slog.String("error", err.Error()))
        } else {
            r.Logger.InfoContext(ctx, "Reconciliation completed",
                slog.Duration("duration", duration),
                slog.Bool("requeue", result.Requeue),
                slog.Duration("requeueAfter", result.RequeueAfter))
        }
    }()
    
    // Fetch object
    object := &MyResourceType{}
    if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
        if errors.IsNotFound(err) {
            r.Logger.InfoContext(ctx, "Resource not found, assuming deleted")
            err = nil
            return
        }
        ctlrutils.LogError(ctx, r.Logger, "Unable to fetch resource", err)
        return
    }
    
    // Add object-specific context
    ctx = ctlrutils.AddObjectContext(ctx, object)
    r.Logger.InfoContext(ctx, "Fetched resource successfully")
    
    // Implementation continues...
}
```

### 2. Phase-Based Logging Pattern

For complex operations with multiple phases:

```go
// Phase 1: Validation
ctx = ctlrutils.LogPhaseStart(ctx, logger, "validation")
phaseStartTime := time.Now()

err := validateInput(ctx)
if err != nil {
    ctlrutils.LogError(ctx, logger, "Validation failed", err)
    return
}
ctlrutils.LogPhaseComplete(ctx, logger, "validation", time.Since(phaseStartTime))

// Phase 2: Processing
ctx = ctlrutils.LogPhaseStart(ctx, logger, "processing")
phaseStartTime = time.Now()
// ... processing logic ...
ctlrutils.LogPhaseComplete(ctx, logger, "processing", time.Since(phaseStartTime))
```

### 3. Error Logging Pattern

Always use structured error logging:

```go
// ❌ Bad - String formatting
logger.ErrorContext(ctx, "Failed to create resource",
    slog.String("error", err.Error()),
    slog.String("name", resourceName))

// ✅ Good - Structured helper
ctlrutils.LogError(ctx, logger, "Failed to create resource", err,
    slog.String("name", resourceName))
```

## Context Management

### Adding Context Attributes

Use `logging.AppendCtx()` to add attributes that will appear in all subsequent logs:

```go
// Add resource-specific context
ctx = logging.AppendCtx(ctx, slog.String("clusterID", clusterID))
ctx = logging.AppendCtx(ctx, slog.String("nodePool", nodePoolName))

// All subsequent logs will include these attributes
logger.InfoContext(ctx, "Processing cluster node")
// Output: {"clusterID":"cluster-123","nodePool":"workers","msg":"Processing cluster node",...}
```

### Standard Attribute Names

Use these standardized attribute names for consistency:

```go
const (
    LogAttrResource        = "resource"        // Resource type being processed
    LogAttrNamespace       = "namespace"       // Kubernetes namespace
    LogAttrResourceVersion = "resourceVersion" // Resource version
    LogAttrGeneration      = "generation"      // Resource generation
    LogAttrError           = "error"           // Error message
    LogAttrDuration        = "duration"        // Operation duration
    LogAttrPhase           = "phase"           // Current processing phase
    LogAttrAction          = "action"          // Action being performed
    LogAttrOperation       = "operation"       // Specific operation
)
```

## Controller Logging

### Logger Setup

Controllers should receive their logger from the properly configured application context:

```go
// ✅ Correct - Use context-propagated logger
Logger: logger.With("controller", "MyController"),

// ❌ Incorrect - Creates new logger instance
Logger: slog.New(logging.NewLoggingContextHandler(slog.LevelInfo)),
```

### Standard Utilities

Use the controller logging utilities for consistent behavior:

```go
// Start reconciliation with standard context
ctx = ctlrutils.LogReconcileStart(ctx, logger, req, "ResourceType")

// Add object metadata to context
ctx = ctlrutils.AddObjectContext(ctx, object)

// Log phase operations
ctx = ctlrutils.LogPhaseStart(ctx, logger, "validation")
ctlrutils.LogPhaseComplete(ctx, logger, "validation", duration)

// Structured error logging
ctlrutils.LogError(ctx, logger, "Operation failed", err, 
    slog.String("key", "value"))

// Operation logging with context
ctx = ctlrutils.LogOperation(ctx, logger, "deploy", "Deploying resources",
    slog.String("target", "production"))
```

## Error Handling

### Error Logging Best Practices

1. **Always include context**: Use `LogError` helper for consistent formatting
2. **Include relevant attributes**: Add operation-specific context
3. **Use appropriate log levels**: ERROR for failures, WARN for recoverable issues
4. **Provide actionable information**: Include details needed for debugging

```go
// ✅ Good error logging
ctlrutils.LogError(ctx, logger, "Failed to deploy cluster", err,
    slog.String("clusterTemplate", templateName),
    slog.String("targetNamespace", namespace),
    slog.Int("retryAttempt", attempt))

// ✅ Good warning logging  
logger.WarnContext(ctx, "Resource not ready, will retry",
    slog.String("resourceType", "ClusterInstance"),
    slog.Duration("nextRetry", retryInterval))
```

### Error Propagation

Maintain context when propagating errors:

```go
if err := someOperation(ctx); err != nil {
    // Add context to error before returning
    return fmt.Errorf("failed to complete phase %s: %w", phaseName, err)
}
```

## Performance Tracking

### Duration Tracking

Track operation durations for performance monitoring:

```go
startTime := time.Now()
defer func() {
    duration := time.Since(startTime)
    logger.InfoContext(ctx, "Operation completed",
        slog.Duration("duration", duration),
        slog.String("operation", "cluster_provision"))
}()
```

### Phase Performance

Track individual phase performance in complex operations:

```go
// Track overall operation
operationStart := time.Now()

// Track individual phases
for _, phase := range phases {
    phaseStart := time.Now()
    ctx = ctlrutils.LogPhaseStart(ctx, logger, phase.Name)
    
    err := phase.Execute(ctx)
    
    phaseDuration := time.Since(phaseStart)
    ctlrutils.LogPhaseComplete(ctx, logger, phase.Name, phaseDuration)
    
    if err != nil {
        ctlrutils.LogError(ctx, logger, "Phase failed", err,
            slog.String("phase", phase.Name),
            slog.Duration("phaseDuration", phaseDuration))
        return
    }
}

totalDuration := time.Since(operationStart)
logger.InfoContext(ctx, "All phases completed",
    slog.Duration("totalDuration", totalDuration),
    slog.Int("phaseCount", len(phases)))
```

## Migration Guide

See [Controller Migration Guide](./logging-migration-guide.md) for detailed migration instructions.

## Best Practices

### 1. Use Structured Attributes

```go
// ❌ Avoid string formatting in logs
logger.InfoContext(ctx, fmt.Sprintf("Processing cluster %s in namespace %s", name, namespace))

// ✅ Use structured attributes
logger.InfoContext(ctx, "Processing cluster",
    slog.String("clusterName", name),
    slog.String("namespace", namespace))
```

### 2. Include Relevant Context

```go
// ✅ Rich context helps debugging
logger.InfoContext(ctx, "Waiting for resource to be ready",
    slog.String("resourceType", "Pod"),
    slog.String("name", podName),
    slog.String("namespace", namespace),
    slog.String("phase", "Pending"),
    slog.Duration("waitTime", time.Since(startTime)))
```

### 3. Use Appropriate Log Levels

- **DEBUG**: Detailed debugging information (not typically enabled in production)
- **INFO**: General information about system operation
- **WARN**: Warning conditions that don't prevent operation
- **ERROR**: Error conditions that prevent successful operation

### 4. Maintain Context Hierarchy

```go
// Build context hierarchically
ctx = logging.AppendCtx(ctx, slog.String("operation", "cluster_provision"))
ctx = logging.AppendCtx(ctx, slog.String("clusterID", clusterID))

// Phase-specific context
ctx = ctlrutils.LogPhaseStart(ctx, logger, "hardware_allocation")
ctx = logging.AppendCtx(ctx, slog.String("hardwareType", "baremetal"))

// All logs now include: operation, clusterID, phase, hardwareType
logger.InfoContext(ctx, "Allocating hardware nodes")
```

## Troubleshooting

### Common Issues

#### 1. Context Attributes Not Appearing

**Problem**: Context attributes added with `logging.AppendCtx()` don't appear in logs.

**Solution**: Ensure the logger is properly configured with `LoggingContextHandler`:

```go
// Check logger setup in main application
logger, err := logging.NewLogger().
    SetWriter(os.Stdout).
    Build()

// Verify controllers use context-propagated logger
Logger: internal.LoggerFromContext(ctx).With("controller", "MyController"),
```

#### 2. Text Format Instead of JSON

**Problem**: Logs appear in text format instead of JSON.

**Solution**: Verify logger configuration uses `slog.NewJSONHandler`:

```go
// In logging.NewLogger().Build()
baseHandler := slog.NewJSONHandler(writer, options)
contextHandler := &LoggingContextHandler{
    handler: baseHandler,
    level:   level,
}
```

#### 3. Missing Phase Context

**Problem**: Phase information not appearing in logs.

**Solution**: Ensure `LogPhaseStart` is called and context is properly propagated:

```go
ctx = ctlrutils.LogPhaseStart(ctx, logger, "validation")
// Use the returned context for all subsequent operations
result := validateInput(ctx) // ✅ Passes phase context
```

### Debugging Logger Configuration

Add this debugging code to verify logger setup:

```go
func debugLoggerSetup(ctx context.Context, logger *slog.Logger) {
    // Test basic logging
    logger.InfoContext(ctx, "Logger test - basic")
    
    // Test context attributes
    ctx = logging.AppendCtx(ctx, slog.String("test", "value"))
    logger.InfoContext(ctx, "Logger test - with context")
    
    // Test phase logging
    ctx = ctlrutils.LogPhaseStart(ctx, logger, "debug_phase")
    logger.InfoContext(ctx, "Logger test - with phase")
}
```

Expected output:

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Logger test - basic"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Logger test - with context","test":"value"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Phase started","test":"value","phase":"debug_phase"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Logger test - with phase","test":"value","phase":"debug_phase"}
```

### Performance Considerations

1. **Attribute Efficiency**: Context attributes are more efficient than repeated `slog` calls
2. **JSON Marshaling**: Large objects should be summarized before logging
3. **Log Level Filtering**: Use appropriate log levels to control verbosity
4. **Context Size**: Avoid adding too many attributes to context

## Examples

See the [Logging Examples Guide](./logging-examples.md) for comprehensive examples of all patterns.

---

For questions or improvements to this documentation, please contact the O-Cloud Manager development team.

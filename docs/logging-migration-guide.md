# Controller Logging Migration Guide

<!--
SPDX-FileCopyrightText: Red Hat
SPDX-License-Identifier: Apache-2.0
Generated-By: Claude/Cursor AI Assistant
-->

## Overview

This guide provides step-by-step instructions for migrating controllers to use the O-Cloud Manager structured logging pattern. The migration process is designed to be incremental and safe, with no breaking changes to existing functionality.

## Table of Contents

1. [Migration Prerequisites](#migration-prerequisites)
2. [Migration Steps](#migration-steps)
3. [Controller Types](#controller-types)
4. [Common Patterns](#common-patterns)
5. [Testing Your Migration](#testing-your-migration)
6. [Validation Checklist](#validation-checklist)
7. [Rollback Procedures](#rollback-procedures)

## Migration Prerequisites

### 1. Verify Logger Setup

Ensure your controller receives a properly configured logger:

```go
// ✅ Correct - From main application context
Logger: internal.LoggerFromContext(ctx).With("controller", "MyController"),

// ❌ Incorrect - Creates new logger instance  
Logger: slog.New(logging.NewLoggingContextHandler(slog.LevelInfo)),
```

### 2. Import Required Packages

Add the controller utilities import:

```go
import (
    // ... existing imports ...
    ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
    "time"  // For duration tracking
)
```

## Migration Steps

### Step 1: Enhance Reconcile Method Signature

**Before:**

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Fetch object
    object := &MyResourceType{}
    if err := r.Client.Get(ctx, req.NamespacedName, object); err != nil {
        if errors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        r.Logger.ErrorContext(ctx, "Unable to fetch resource",
            slog.String("error", err.Error()))
        return ctrl.Result{}, err
    }
    
    // ... rest of reconciliation
}
```

**After:**

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
    
    // ... rest of reconciliation
}
```

### Step 2: Add Phase-Based Logging

For controllers with multiple processing phases:

**Before:**

```go
// Validate input
err := validateInput(object)
if err != nil {
    r.Logger.ErrorContext(ctx, "Validation failed", slog.String("error", err.Error()))
    return ctrl.Result{}, err
}

// Process resource  
err = processResource(object)
if err != nil {
    r.Logger.ErrorContext(ctx, "Processing failed", slog.String("error", err.Error()))
    return ctrl.Result{}, err
}
```

**After:**

```go
// Phase 1: Validation
ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "validation")
phaseStartTime := time.Now()

err = validateInput(object)
if err != nil {
    ctlrutils.LogError(ctx, r.Logger, "Validation failed", err)
    return
}
ctlrutils.LogPhaseComplete(ctx, r.Logger, "validation", time.Since(phaseStartTime))

// Phase 2: Processing
ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "processing")
phaseStartTime = time.Now()

err = processResource(object)
if err != nil {
    ctlrutils.LogError(ctx, r.Logger, "Processing failed", err)
    return
}
ctlrutils.LogPhaseComplete(ctx, r.Logger, "processing", time.Since(phaseStartTime))
```

### Step 3: Replace String Formatting

**Before:**

```go
r.Logger.InfoContext(ctx, fmt.Sprintf("Processing cluster %s", clusterName))
r.Logger.ErrorContext(ctx, "Failed to create resource",
    slog.String("name", resourceName),
    slog.String("error", err.Error()))
```

**After:**

```go
r.Logger.InfoContext(ctx, "Processing cluster",
    slog.String("clusterName", clusterName))
ctlrutils.LogError(ctx, r.Logger, "Failed to create resource", err,
    slog.String("name", resourceName))
```

### Step 4: Add Context Enrichment

**Before:**

```go
r.Logger.InfoContext(ctx, "Starting hardware provisioning")
// ... hardware provisioning logic ...
r.Logger.InfoContext(ctx, "Hardware provisioning completed")
```

**After:**

```go
// Add hardware-specific context
ctx = logging.AppendCtx(ctx, slog.String("hardwareType", "baremetal"))
ctx = logging.AppendCtx(ctx, slog.String("nodeCount", strconv.Itoa(nodeCount)))

r.Logger.InfoContext(ctx, "Starting hardware provisioning")
// ... hardware provisioning logic ...
r.Logger.InfoContext(ctx, "Hardware provisioning completed")
// All logs now include hardwareType and nodeCount automatically
```

## Controller Types

### Simple Controllers (1-2 phases)

For controllers with simple logic:

```go
func (r *SimpleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "SimpleResource")
    
    defer func() {
        duration := time.Since(startTime)
        if err != nil {
            r.Logger.ErrorContext(ctx, "Reconciliation failed",
                slog.Duration("duration", duration),
                slog.String("error", err.Error()))
        } else {
            r.Logger.InfoContext(ctx, "Reconciliation completed",
                slog.Duration("duration", duration))
        }
    }()
    
    // Fetch and process
    object := &SimpleResourceType{}
    if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
        if errors.IsNotFound(err) {
            r.Logger.InfoContext(ctx, "Resource not found, assuming deleted")
            err = nil
            return
        }
        ctlrutils.LogError(ctx, r.Logger, "Unable to fetch resource", err)
        return
    }
    
    ctx = ctlrutils.AddObjectContext(ctx, object)
    r.Logger.InfoContext(ctx, "Processing simple resource")
    
    // Simple processing logic
    err = r.processSimpleResource(ctx, object)
    return
}
```

### Complex Controllers (3+ phases)

For controllers with multiple distinct phases:

```go
func (r *ComplexReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "ComplexResource")
    
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
    object := &ComplexResourceType{}
    if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
        if errors.IsNotFound(err) {
            r.Logger.InfoContext(ctx, "Resource not found, assuming deleted")
            err = nil
            return
        }
        ctlrutils.LogError(ctx, r.Logger, "Unable to fetch resource", err)
        return
    }
    
    ctx = ctlrutils.AddObjectContext(ctx, object)
    r.Logger.InfoContext(ctx, "Fetched resource successfully")
    
    // Phase 1: Validation
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "validation")
    phaseStartTime := time.Now()
    
    if err = r.validateResource(ctx, object); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Validation failed", err)
        return
    }
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "validation", time.Since(phaseStartTime))
    
    // Phase 2: Resource Creation
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "resource_creation")
    phaseStartTime = time.Now()
    
    if err = r.createResources(ctx, object); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Resource creation failed", err)
        return
    }
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "resource_creation", time.Since(phaseStartTime))
    
    // Phase 3: Status Update
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "status_update")
    phaseStartTime = time.Now()
    
    if err = r.updateStatus(ctx, object); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Status update failed", err)
        return
    }
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "status_update", time.Since(phaseStartTime))
    
    return
}
```

## Common Patterns

### 1. Deletion Handling

```go
if object.GetDeletionTimestamp() != nil {
    r.Logger.InfoContext(ctx, "Resource is being deleted")
    
    if controllerutil.ContainsFinalizer(object, MyFinalizer) {
        ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "deletion_cleanup")
        
        if err = r.performCleanup(ctx, object); err != nil {
            ctlrutils.LogError(ctx, r.Logger, "Cleanup failed", err)
            return
        }
        
        controllerutil.RemoveFinalizer(object, MyFinalizer)
        if err = r.Update(ctx, object); err != nil {
            ctlrutils.LogError(ctx, r.Logger, "Failed to remove finalizer", err)
            return
        }
        
        r.Logger.InfoContext(ctx, "Resource cleanup completed")
    }
    return
}
```

### 2. Conditional Processing

```go
if needsProcessing(object) {
    ctx = ctlrutils.LogOperation(ctx, r.Logger, "conditional_process", 
        "Starting conditional processing",
        slog.String("condition", "resource_changed"))
    
    if err = r.processChanges(ctx, object); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Conditional processing failed", err)
        return
    }
} else {
    r.Logger.InfoContext(ctx, "No processing needed, resource up to date")
}
```

### 3. Retry Logic

```go
const maxRetries = 3

for attempt := 1; attempt <= maxRetries; attempt++ {
    ctx = logging.AppendCtx(ctx, slog.Int("attempt", attempt))
    
    if err = r.performOperation(ctx, object); err == nil {
        break
    }
    
    if attempt == maxRetries {
        ctlrutils.LogError(ctx, r.Logger, "Operation failed after all retries", err,
            slog.Int("maxRetries", maxRetries))
        return
    }
    
    r.Logger.WarnContext(ctx, "Operation failed, retrying",
        slog.String("error", err.Error()),
        slog.Int("nextAttempt", attempt+1))
    
    time.Sleep(time.Duration(attempt) * time.Second)
}

r.Logger.InfoContext(ctx, "Operation succeeded",
    slog.Int("attempts", attempt))
```

## Testing Your Migration

### 1. Unit Tests

Verify your controller utilities work correctly:

```go
func TestControllerLogging(t *testing.T) {
    var buf bytes.Buffer
    logger, err := logging.NewLogger().
        SetWriter(&buf).
        Build()
    require.NoError(t, err)
    
    req := ctrl.Request{
        NamespacedName: types.NamespacedName{
            Name:      "test-resource",
            Namespace: "test-namespace",
        },
    }
    
    ctx := context.Background()
    ctx = ctlrutils.LogReconcileStart(ctx, logger, req, "TestResource")
    
    logger.InfoContext(ctx, "Test message")
    
    // Parse and validate log output
    output := buf.String()
    assert.Contains(t, output, "test-resource")
    assert.Contains(t, output, "test-namespace")
    assert.Contains(t, output, "TestResource")
}
```

### 2. Integration Tests

Test full reconciliation with logging:

```go
func TestReconcileWithLogging(t *testing.T) {
    var buf bytes.Buffer
    logger, _ := logging.NewLogger().SetWriter(&buf).Build()
    
    reconciler := &MyReconciler{
        Client: fakeClient,
        Logger: logger,
    }
    
    req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"}}
    result, err := reconciler.Reconcile(context.Background(), req)
    
    // Verify reconciliation worked
    assert.NoError(t, err)
    assert.False(t, result.Requeue)
    
    // Verify logging output
    output := buf.String()
    assert.Contains(t, output, "Starting reconciliation")
    assert.Contains(t, output, "Reconciliation completed")
    assert.Contains(t, output, "duration")
}
```

### 3. Manual Testing

Run your controller with debug logging to verify output:

```bash
# Set debug level
export LOG_LEVEL=debug

# Run controller
go run ./cmd/controller-manager

# Check log output format
kubectl logs -f deployment/my-controller | jq .
```

## Validation Checklist

Before considering migration complete, verify:

### ✅ Basic Functionality

- [ ] Controller starts without errors
- [ ] Reconciliation logic still works correctly
- [ ] All existing tests pass
- [ ] No regressions in functionality

### ✅ Logging Output

- [ ] Logs are in JSON format
- [ ] Standard reconciliation logs appear
- [ ] Context attributes are included
- [ ] Phase logging works (if applicable)
- [ ] Error logging is structured
- [ ] Duration tracking is present

### ✅ Context Propagation

- [ ] `LogReconcileStart` adds standard context
- [ ] `AddObjectContext` includes resource metadata
- [ ] `logging.AppendCtx` attributes appear in logs
- [ ] Phase context is maintained throughout

### ✅ Performance

- [ ] No significant performance regression
- [ ] Log volume is reasonable
- [ ] Context attributes don't cause memory leaks

### ✅ Error Handling

- [ ] Errors are logged with full context
- [ ] Error messages are actionable
- [ ] Stack traces are preserved where needed

## Example Migration Output

### Before Migration

```text
time="2025-08-14T15:23:36Z" level=info msg="Reconciling MyResource" name=test-resource
time="2025-08-14T15:23:37Z" level=error msg="Failed to create resource: connection refused"
```

### After Migration

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Starting reconciliation","resource":"MyResource","name":"test-resource","namespace":"default","action":"reconcile_start"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Fetched resource successfully","resource":"MyResource","name":"test-resource","namespace":"default","resourceVersion":"123","generation":1}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Phase started","resource":"MyResource","name":"test-resource","namespace":"default","phase":"validation"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Phase completed","resource":"MyResource","name":"test-resource","namespace":"default","phase":"validation","duration":"1.2s"}
{"time":"2025-08-14T15:23:38Z","level":"ERROR","msg":"Resource creation failed","resource":"MyResource","name":"test-resource","namespace":"default","error":"connection refused","phase":"creation"}
{"time":"2025-08-14T15:23:38Z","level":"ERROR","msg":"Reconciliation failed","resource":"MyResource","name":"test-resource","namespace":"default","duration":"2.1s","error":"connection refused"}
```

## Rollback Procedures

If issues arise during migration:

### 1. Immediate Rollback

Revert to the previous controller implementation:

```bash
# Revert changes
git checkout HEAD~1 -- internal/controllers/my_controller.go

# Rebuild and redeploy
make build deploy
```

### 2. Partial Rollback

Keep new logging utilities but remove phase logging:

```go
// Keep basic reconciliation pattern
ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "MyResource")

// Remove phase logging
// ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "validation") // Remove this

// Keep error logging improvements
ctlrutils.LogError(ctx, r.Logger, "Operation failed", err)
```

### 3. Logger Configuration Rollback

If JSON logging causes issues, temporarily disable context handler:

```go
// In logger.go Build() method - temporary rollback
return slog.New(slog.NewJSONHandler(writer, options)), nil
// Skip LoggingContextHandler wrapper temporarily
```

## Getting Help

If you encounter issues during migration:

1. **Check Examples**: Review successfully migrated controllers for patterns
2. **Run Tests**: Use the test utilities to validate your implementation
3. **Debug Output**: Add temporary debug logging to verify context propagation
4. **Ask Questions**: Contact the O-Cloud Manager development team for guidance

---

This migration guide should help you successfully adopt the structured logging pattern in your controllers. The migration is designed to be incremental and safe, with clear rollback options if needed.

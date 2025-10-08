# O-Cloud Manager Logging Quick Reference

<!--
SPDX-FileCopyrightText: Red Hat
SPDX-License-Identifier: Apache-2.0
Generated-By: Claude/Cursor AI Assistant
-->

## Quick Setup

### 1. Controller Setup Template

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    
    // Standard reconciliation setup
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "ResourceType")
    
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
    
    // Add object context
    ctx = ctlrutils.AddObjectContext(ctx, object)
    r.Logger.InfoContext(ctx, "Fetched resource successfully")
    
    // Your reconciliation logic here...
    
    return
}
```

### 2. Required Imports

```go
import (
    "context"
    "time"
    "log/slog"
    
    "k8s.io/apimachinery/pkg/api/errors"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    
    ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
    "github.com/openshift-kni/oran-o2ims/internal/logging"
)
```

## Common Patterns

### Phase Logging

```go
// Start phase
ctx = ctlrutils.LogPhaseStart(ctx, logger, "validation")
phaseStartTime := time.Now()

// Your phase logic here...

// Complete phase
ctlrutils.LogPhaseComplete(ctx, logger, "validation", time.Since(phaseStartTime))
```

### Error Logging

```go
// Standard error logging
ctlrutils.LogError(ctx, logger, "Operation failed", err)

// Error logging with additional context
ctlrutils.LogError(ctx, logger, "Operation failed", err,
    slog.String("operation", "create"),
    slog.String("resource", "ConfigMap"))
```

### Context Enrichment

```go
// Add attributes to context
ctx = logging.AppendCtx(ctx, slog.String("clusterID", clusterID))
ctx = logging.AppendCtx(ctx, slog.Int("nodeCount", nodeCount))

// All subsequent logs include these attributes automatically
logger.InfoContext(ctx, "Processing cluster")
```

### Operation Logging

```go
// Log specific operations
ctx = ctlrutils.LogOperation(ctx, logger, "database_query", "Querying dependencies",
    slog.String("table", "clusters"))
```

## Standard Attributes

| Attribute | Usage | Example |
|-----------|-------|---------|
| `resource` | Resource type being processed | `"ClusterTemplate"` |
| `namespace` | Kubernetes namespace | `"default"` |
| `name` | Resource name | `"my-cluster"` |
| `resourceVersion` | Resource version | `"12345"` |
| `generation` | Resource generation | `3` |
| `phase` | Current processing phase | `"validation"` |
| `action` | Action being performed | `"reconcile_start"` |
| `operation` | Specific operation | `"create_configmap"` |
| `duration` | Operation duration | `"1.5s"` |
| `error` | Error message | `"connection refused"` |

## Logger Configuration

### Correct Logger Setup

```go
// ✅ Correct - Use context-propagated logger
Logger: logger.With("controller", "MyController"),

// ❌ Incorrect - Creates new instance
Logger: slog.New(logging.NewLoggingContextHandler(slog.LevelInfo)),
```

## Log Levels

| Level | When to Use | Example |
|-------|-------------|---------|
| `DEBUG` | Detailed debugging information | Variable values, function entry/exit |
| `INFO` | General operational information | Operation start/complete, status changes |
| `WARN` | Warning conditions | Retries, fallback behavior |
| `ERROR` | Error conditions | Operation failures, exceptions |

## Common Mistakes

### ❌ Don't Do This

```go
// String formatting in messages
logger.InfoContext(ctx, fmt.Sprintf("Processing %s", name))

// Manual error attribute formatting  
logger.ErrorContext(ctx, "Failed", slog.String("error", err.Error()))

// Missing context propagation
logger.InfoContext(context.Background(), "Message")

// Creating new logger instances
slog.New(logging.NewLoggingContextHandler(slog.LevelInfo))
```

### ✅ Do This Instead

```go
// Structured attributes
logger.InfoContext(ctx, "Processing resource", slog.String("name", name))

// Use LogError helper
ctlrutils.LogError(ctx, logger, "Operation failed", err)

// Propagate context
logger.InfoContext(ctx, "Message")

// Use context-propagated logger
logger.With("controller", "MyController")
```

## Troubleshooting

### Context Not Working

```bash
# Check logger configuration
grep -r "LoggingContextHandler" internal/logging/

# Verify controller logger setup
grep -r "logger.With" internal/cmd/
```

### Text Format Instead of JSON

Check that controllers use the context-propagated logger:

```go
// In controller manager setup
Logger: internal.LoggerFromContext(ctx).With("controller", "MyController"),
```

### Missing Attributes

Ensure context is propagated through function calls:

```go
func (r *MyReconciler) someMethod(ctx context.Context, obj *MyType) error {
    // Use the passed context, don't create new one
    logger.InfoContext(ctx, "Processing") // ✅
    // logger.InfoContext(context.Background(), "Processing") // ❌
}
```

## Performance Tips

1. **Build context hierarchically** - Add common attributes early
2. **Use appropriate log levels** - Don't log everything at INFO
3. **Avoid large objects** - Summarize complex data structures
4. **Context efficiency** - Context attributes are more efficient than repeated slog calls

## Testing

### Unit Test Example

```go
func TestControllerLogging(t *testing.T) {
    var buf bytes.Buffer
    logger, err := logging.NewLogger().SetWriter(&buf).Build()
    require.NoError(t, err)
    
    ctx := context.Background()
    ctx = ctlrutils.LogReconcileStart(ctx, logger, ctrl.Request{
        NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
    }, "TestResource")
    
    logger.InfoContext(ctx, "Test message")
    
    output := buf.String()
    assert.Contains(t, output, "test")
    assert.Contains(t, output, "TestResource")
    assert.Contains(t, output, "reconcile_start")
}
```

## Links

- [Full Documentation](./logging-standards.md)
- [Migration Guide](./logging-migration-guide.md)
- [Complete Examples](./logging-examples.md)

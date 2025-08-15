# O-Cloud Manager Documentation

<!--
SPDX-FileCopyrightText: Red Hat
SPDX-License-Identifier: Apache-2.0
Generated-By: Claude/Cursor AI Assistant
-->

## Overview

This directory contains comprehensive documentation for the O-Cloud Manager project, with a focus on our structured logging implementation and best practices.

## 📚 Documentation Index

### 🔧 Logging & Observability

#### **[Logging Standards](./logging-standards.md)**

Comprehensive guide to O-Cloud Manager structured logging architecture, patterns, and best practices.

**Topics covered:**

- Logging Architecture & Core Components
- Standard Controller Patterns  
- Context Management & Attribute Propagation
- Phase-Based Logging for Complex Operations
- Error Handling & Performance Tracking
- Troubleshooting & Configuration

#### **[Migration Guide](./logging-migration-guide.md)**

Step-by-step instructions for migrating controllers to the structured logging pattern.

**Topics covered:**

- Migration Prerequisites & Setup
- Controller Migration Steps (Simple & Complex)
- Common Patterns & Integration Examples  
- Testing & Validation Checklist
- Rollback Procedures & Troubleshooting

#### **[Logging Examples](./logging-examples.md)**

Comprehensive examples showing structured logging patterns in action.

**Topics covered:**

- Basic Controller Pattern Examples
- Complex Multi-Phase Controllers  
- Hardware Plugin Controller Examples
- Error Handling & Performance Monitoring
- Context Enrichment & Integration Patterns

#### **[Quick Reference](./logging-quick-reference.md)**

Fast reference for common logging patterns and troubleshooting.

**Topics covered:**

- Controller Setup Templates
- Common Patterns & Standard Attributes
- Configuration & Troubleshooting
- Performance Tips & Testing

## 🎯 Quick Start

### For New Controllers

1. **Follow the template** from [Quick Reference](./logging-quick-reference.md#quick-setup)
2. **Use standard patterns** from [Logging Examples](./logging-examples.md#basic-controller-pattern)
3. **Test your implementation** using the [Migration Guide](./logging-migration-guide.md#testing-your-migration)

### For Existing Controllers

1. **Read the migration guide** - [Migration Guide](./logging-migration-guide.md)
2. **Choose your migration approach** - Gradual or complete
3. **Validate your changes** using the [checklist](./logging-migration-guide.md#validation-checklist)

### For Troubleshooting

1. **Check common issues** in [Troubleshooting](./logging-standards.md#troubleshooting)
2. **Use the quick reference** for [common mistakes](./logging-quick-reference.md#common-mistakes)
3. **Review examples** for [proper patterns](./logging-examples.md)

## 🏗️ Architecture Overview

### Logging Components

```text
Application Context
        ↓
  LoggerBuilder → slog.Logger (JSON)
        ↓
LoggingContextHandler → Context Extraction
        ↓
Controller Utilities → Standard Patterns
        ↓
  Structured JSON Logs
```

### Key Benefits

- **🔍 Enhanced Debugging**: Rich context in every log entry
- **📊 Performance Insights**: Duration tracking and phase monitoring  
- **🔗 Request Correlation**: Follow operations from start to finish
- **📈 Observability**: Structured data enables powerful analytics
- **🎯 Production Ready**: Enterprise-grade logging and monitoring

## 📋 Migration Status

### ✅ Completed Controllers

| Controller | Status | Features |
|------------|--------|----------|
| **ProvisioningRequest** | ✅ Complete | Multi-phase logging, duration tracking, rich context |
| **Inventory** | ✅ Complete | Infrastructure deployment phases, server setup tracking |
| **HardwarePlugin** | ✅ Complete | Validation phases, plugin connectivity logging |
| **NodeAllocationRequest** | ✅ Complete | Hardware provisioning workflow, node tracking |
| **AllocatedNode** | ✅ Complete | Node lifecycle management, allocation tracking |

### 🎉 Results Achieved

- **End-to-end observability** across the entire O-Cloud Manager stack
- **Consistent JSON logging** with context attribute extraction
- **Rich debugging information** for production troubleshooting
- **Performance monitoring** with phase-level duration tracking
- **Zero breaking changes** to existing functionality

## 🔧 Implementation Highlights

### Standard Controller Pattern

Every controller now follows this proven pattern:

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "ResourceType")
    
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
    
    // Implementation with rich context...
}
```

### Sample Log Output

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Starting reconciliation","resource":"ProvisioningRequest","name":"cluster-001","namespace":"clusters","action":"reconcile_start"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Phase started","resource":"ProvisioningRequest","name":"cluster-001","namespace":"clusters","phase":"pre_provisioning"}
{"time":"2025-08-14T15:23:45Z","level":"INFO","msg":"Phase completed","resource":"ProvisioningRequest","name":"cluster-001","namespace":"clusters","phase":"pre_provisioning","duration":"8.7s"}
{"time":"2025-08-14T15:25:12Z","level":"INFO","msg":"Reconciliation completed","resource":"ProvisioningRequest","name":"cluster-001","namespace":"clusters","duration":"1m36s","requeue":false}
```

## 🚀 Next Steps

### For Development Teams

1. **Adopt the patterns** - Use our established logging standards for new controllers
2. **Migrate existing code** - Follow the migration guide for older controllers  
3. **Enhance with context** - Add domain-specific context to improve debugging
4. **Monitor and tune** - Use the structured data for performance optimization

### For Operations Teams

1. **Set up log aggregation** - Collect structured JSON logs for analysis
2. **Create dashboards** - Use the rich attributes for monitoring dashboards
3. **Configure alerts** - Set up alerts based on duration and error patterns
4. **Performance monitoring** - Track phase durations and identify bottlenecks

### Future Enhancements

The structured logging foundation enables future enhancements:

- **🔗 Request Correlation IDs** - End-to-end request tracing
- **📊 Advanced Analytics** - Machine learning on structured log data
- **🎯 Predictive Monitoring** - Proactive issue detection
- **🔧 Auto-remediation** - Automated responses to common issues

## 📞 Support & Contributing

### Getting Help

- **Documentation Issues**: Check the troubleshooting sections in each guide
- **Implementation Questions**: Review the examples and migration guide
- **Technical Support**: Contact the O-Cloud Manager development team

### Contributing

Improvements to documentation are welcome! Please:

1. Follow the existing structure and style
2. Include practical examples
3. Test any code examples  
4. Update the quick reference as needed

### Standards

- All documentation uses Markdown format
- Code examples must be tested and working
- Include expected log output for examples
- Maintain the "Generated-By" header for AI-assisted content

---

## 📖 Document History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2025-08-14 | Initial comprehensive logging documentation |

**Generated-By**: Claude/Cursor AI Assistant  
**Maintained-By**: O-Cloud Manager Development Team

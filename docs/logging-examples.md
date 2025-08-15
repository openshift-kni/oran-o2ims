# O-Cloud Manager Logging Examples

<!--
SPDX-FileCopyrightText: Red Hat
SPDX-License-Identifier: Apache-2.0
Generated-By: Claude/Cursor AI Assistant
-->

## Overview

This document provides comprehensive examples of the O-Cloud Manager structured logging patterns in action. Each example shows both the code implementation and the expected JSON log output.

## Table of Contents

1. [Basic Controller Pattern](#basic-controller-pattern)
2. [Complex Multi-Phase Controller](#complex-multi-phase-controller)
3. [Hardware Plugin Controller](#hardware-plugin-controller)
4. [Error Handling Examples](#error-handling-examples)
5. [Performance Monitoring](#performance-monitoring)
6. [Context Enrichment](#context-enrichment)
7. [Integration with Existing Code](#integration-with-existing-code)

## Basic Controller Pattern

### Simple Resource Controller

```go
// Simple controller for basic resources
func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    
    // Add standard reconciliation context
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "ConfigMap")
    
    defer func() {
        duration := time.Since(startTime)
        if err != nil {
            r.Logger.ErrorContext(ctx, "Reconciliation failed",
                slog.Duration("duration", duration),
                slog.String("error", err.Error()))
        } else {
            r.Logger.InfoContext(ctx, "Reconciliation completed",
                slog.Duration("duration", duration),
                slog.Bool("requeue", result.Requeue))
        }
    }()
    
    // Fetch the ConfigMap
    configMap := &corev1.ConfigMap{}
    if err = r.Client.Get(ctx, req.NamespacedName, configMap); err != nil {
        if errors.IsNotFound(err) {
            r.Logger.InfoContext(ctx, "ConfigMap not found, assuming deleted")
            err = nil
            return
        }
        ctlrutils.LogError(ctx, r.Logger, "Unable to fetch ConfigMap", err)
        return
    }
    
    // Add object-specific context
    ctx = ctlrutils.AddObjectContext(ctx, configMap)
    r.Logger.InfoContext(ctx, "Fetched ConfigMap successfully")
    
    // Simple validation
    if err = r.validateConfigMap(ctx, configMap); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "ConfigMap validation failed", err)
        return
    }
    
    r.Logger.InfoContext(ctx, "ConfigMap is valid")
    return
}
```

**Expected Log Output:**

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Starting reconciliation","resource":"ConfigMap","name":"my-config","namespace":"default","action":"reconcile_start"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Fetched ConfigMap successfully","resource":"ConfigMap","name":"my-config","namespace":"default","resourceVersion":"12345","generation":1}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"ConfigMap is valid","resource":"ConfigMap","name":"my-config","namespace":"default","resourceVersion":"12345","generation":1}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Reconciliation completed","resource":"ConfigMap","name":"my-config","namespace":"default","duration":"1.2s","requeue":false}
```

## Complex Multi-Phase Controller

### Cluster Provisioning Controller

```go
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    result = ctrl.Result{RequeueAfter: 5 * time.Minute}
    
    // Add standard reconciliation context
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "Cluster")
    
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
    
    // Fetch cluster
    cluster := &ClusterType{}
    if err = r.Client.Get(ctx, req.NamespacedName, cluster); err != nil {
        if errors.IsNotFound(err) {
            r.Logger.InfoContext(ctx, "Cluster not found, assuming deleted")
            err = nil
            return
        }
        ctlrutils.LogError(ctx, r.Logger, "Unable to fetch Cluster", err)
        return
    }
    
    // Add object and cluster-specific context
    ctx = ctlrutils.AddObjectContext(ctx, cluster)
    ctx = logging.AppendCtx(ctx, slog.String("clusterType", cluster.Spec.Type))
    ctx = logging.AppendCtx(ctx, slog.Int("nodeCount", cluster.Spec.NodeCount))
    r.Logger.InfoContext(ctx, "Fetched Cluster successfully")
    
    // Phase 1: Validation
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "validation")
    phaseStartTime := time.Now()
    
    if err = r.validateClusterSpec(ctx, cluster); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Cluster validation failed", err)
        return
    }
    r.Logger.InfoContext(ctx, "Cluster specification validated")
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "validation", time.Since(phaseStartTime))
    
    // Phase 2: Infrastructure Preparation
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "infrastructure_prep")
    phaseStartTime = time.Now()
    
    infraReady, err := r.prepareInfrastructure(ctx, cluster)
    if err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Infrastructure preparation failed", err)
        return
    }
    
    if !infraReady {
        r.Logger.InfoContext(ctx, "Infrastructure not ready, requeueing",
            slog.Duration("requeueAfter", 2*time.Minute))
        result.RequeueAfter = 2 * time.Minute
        return
    }
    
    r.Logger.InfoContext(ctx, "Infrastructure preparation completed")
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "infrastructure_prep", time.Since(phaseStartTime))
    
    // Phase 3: Node Provisioning
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "node_provisioning")
    phaseStartTime = time.Now()
    
    nodesReady, err := r.provisionNodes(ctx, cluster)
    if err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Node provisioning failed", err)
        return
    }
    
    if !nodesReady {
        r.Logger.InfoContext(ctx, "Nodes not ready, requeueing",
            slog.Duration("requeueAfter", 5*time.Minute))
        result.RequeueAfter = 5 * time.Minute
        return
    }
    
    ctx = logging.AppendCtx(ctx, slog.Int("readyNodes", cluster.Status.ReadyNodes))
    r.Logger.InfoContext(ctx, "Node provisioning completed")
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "node_provisioning", time.Since(phaseStartTime))
    
    // Phase 4: Configuration
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "configuration")
    phaseStartTime = time.Now()
    
    if err = r.configureCluster(ctx, cluster); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Cluster configuration failed", err)
        return
    }
    
    r.Logger.InfoContext(ctx, "Cluster configuration completed")
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "configuration", time.Since(phaseStartTime))
    
    // Update status and complete
    cluster.Status.Phase = "Ready"
    if err = r.Status().Update(ctx, cluster); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Failed to update cluster status", err)
        return
    }
    
    r.Logger.InfoContext(ctx, "Cluster is ready")
    result.RequeueAfter = 0 // No more requeuing needed
    return
}
```

**Expected Log Output:**

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Starting reconciliation","resource":"Cluster","name":"prod-cluster","namespace":"clusters","action":"reconcile_start"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Fetched Cluster successfully","resource":"Cluster","name":"prod-cluster","namespace":"clusters","resourceVersion":"54321","generation":2,"clusterType":"production","nodeCount":5}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Phase started","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"phase":"validation"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Cluster specification validated","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"phase":"validation"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Phase completed","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"phase":"validation","duration":"1.2s"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Phase started","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"phase":"infrastructure_prep"}
{"time":"2025-08-14T15:23:45Z","level":"INFO","msg":"Infrastructure preparation completed","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"phase":"infrastructure_prep"}
{"time":"2025-08-14T15:23:45Z","level":"INFO","msg":"Phase completed","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"phase":"infrastructure_prep","duration":"8.1s"}
{"time":"2025-08-14T15:25:12Z","level":"INFO","msg":"Node provisioning completed","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"phase":"node_provisioning","readyNodes":5}
{"time":"2025-08-14T15:26:34Z","level":"INFO","msg":"Cluster is ready","resource":"Cluster","name":"prod-cluster","namespace":"clusters","clusterType":"production","nodeCount":5,"readyNodes":5}
{"time":"2025-08-14T15:26:34Z","level":"INFO","msg":"Reconciliation completed","resource":"Cluster","name":"prod-cluster","namespace":"clusters","duration":"3m58s","requeue":false,"requeueAfter":"0s"}
```

## Hardware Plugin Controller

### NodeAllocationRequest Controller

```go
func (r *NodeAllocationRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    
    // Add standard reconciliation context
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "NodeAllocationRequest")
    
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
    
    // Fetch NodeAllocationRequest
    nodeRequest := &NodeAllocationRequestType{}
    if err = r.Client.Get(ctx, req.NamespacedName, nodeRequest); err != nil {
        if errors.IsNotFound(err) {
            r.Logger.InfoContext(ctx, "NodeAllocationRequest not found, assuming deleted")
            err = nil
            return
        }
        ctlrutils.LogError(ctx, r.Logger, "Unable to fetch NodeAllocationRequest", err)
        return
    }
    
    // Add object and hardware-specific context
    ctx = ctlrutils.AddObjectContext(ctx, nodeRequest)
    ctx = logging.AppendCtx(ctx, slog.String("clusterID", nodeRequest.Spec.ClusterID))
    ctx = logging.AppendCtx(ctx, slog.String("hardwareProfile", nodeRequest.Spec.HardwareProfile))
    ctx = logging.AppendCtx(ctx, slog.Int("requestedNodes", nodeRequest.Spec.NodeCount))
    r.Logger.InfoContext(ctx, "Fetched NodeAllocationRequest successfully")
    
    // Check if hardware plugin is available
    ctx = ctlrutils.LogOperation(ctx, r.Logger, "plugin_check", "Checking hardware plugin availability")
    
    plugin, err := r.getHardwarePlugin(ctx, nodeRequest.Spec.HardwareProfile)
    if err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Hardware plugin not available", err,
            slog.String("hardwareProfile", nodeRequest.Spec.HardwareProfile))
        return
    }
    
    ctx = logging.AppendCtx(ctx, slog.String("pluginVersion", plugin.Status.Version))
    r.Logger.InfoContext(ctx, "Hardware plugin available")
    
    // Phase 1: Node Discovery
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "node_discovery")
    phaseStartTime := time.Now()
    
    availableNodes, err := r.discoverAvailableNodes(ctx, nodeRequest)
    if err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Node discovery failed", err)
        return
    }
    
    ctx = logging.AppendCtx(ctx, slog.Int("availableNodes", len(availableNodes)))
    r.Logger.InfoContext(ctx, "Node discovery completed")
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "node_discovery", time.Since(phaseStartTime))
    
    // Check if enough nodes are available
    if len(availableNodes) < nodeRequest.Spec.NodeCount {
        r.Logger.WarnContext(ctx, "Insufficient nodes available",
            slog.Int("required", nodeRequest.Spec.NodeCount),
            slog.Int("available", len(availableNodes)))
        result.RequeueAfter = 5 * time.Minute
        return
    }
    
    // Phase 2: Node Allocation
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "node_allocation")
    phaseStartTime = time.Now()
    
    allocatedNodes, err := r.allocateNodes(ctx, nodeRequest, availableNodes)
    if err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Node allocation failed", err)
        return
    }
    
    ctx = logging.AppendCtx(ctx, slog.Int("allocatedNodes", len(allocatedNodes)))
    for i, node := range allocatedNodes {
        ctx = logging.AppendCtx(ctx, slog.String(fmt.Sprintf("node%d", i), node.Name))
    }
    
    r.Logger.InfoContext(ctx, "Node allocation completed")
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "node_allocation", time.Since(phaseStartTime))
    
    // Phase 3: Configuration
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "node_configuration")
    phaseStartTime = time.Now()
    
    if err = r.configureAllocatedNodes(ctx, nodeRequest, allocatedNodes); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Node configuration failed", err)
        return
    }
    
    r.Logger.InfoContext(ctx, "Node configuration completed")
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "node_configuration", time.Since(phaseStartTime))
    
    // Update status
    nodeRequest.Status.AllocatedNodeCount = len(allocatedNodes)
    nodeRequest.Status.Phase = "Allocated"
    
    if err = r.Status().Update(ctx, nodeRequest); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Failed to update NodeAllocationRequest status", err)
        return
    }
    
    r.Logger.InfoContext(ctx, "NodeAllocationRequest processing completed successfully")
    return
}
```

**Expected Log Output:**

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Starting reconciliation","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","action":"reconcile_start"}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Fetched NodeAllocationRequest successfully","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","resourceVersion":"67890","generation":1,"clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3}
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Checking hardware plugin availability","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3,"operation":"plugin_check"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Hardware plugin available","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3,"pluginVersion":"v1.2.3"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Phase started","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3,"pluginVersion":"v1.2.3","phase":"node_discovery"}
{"time":"2025-08-14T15:23:42Z","level":"INFO","msg":"Node discovery completed","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3,"pluginVersion":"v1.2.3","phase":"node_discovery","availableNodes":5}
{"time":"2025-08-14T15:23:42Z","level":"INFO","msg":"Phase completed","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3,"pluginVersion":"v1.2.3","phase":"node_discovery","duration":"5.1s"}
{"time":"2025-08-14T15:23:45Z","level":"INFO","msg":"Node allocation completed","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3,"pluginVersion":"v1.2.3","phase":"node_allocation","allocatedNodes":3,"node0":"worker-001","node1":"worker-002","node2":"worker-003"}
{"time":"2025-08-14T15:23:52Z","level":"INFO","msg":"NodeAllocationRequest processing completed successfully","resource":"NodeAllocationRequest","name":"cluster-nodes","namespace":"hwmgr","clusterID":"prod-cluster-001","hardwareProfile":"baremetal-large","requestedNodes":3,"pluginVersion":"v1.2.3","allocatedNodes":3}
```

## Error Handling Examples

### Comprehensive Error Scenarios

```go
func (r *MyReconciler) handleErrors(ctx context.Context, object *MyResource) error {
    // 1. Validation Error with Context
    if err := r.validateResource(ctx, object); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Resource validation failed", err,
            slog.String("validationType", "schema"),
            slog.String("resourceSpec", object.Spec.Type))
        return err
    }
    
    // 2. External Service Error with Retry Context
    const maxRetries = 3
    for attempt := 1; attempt <= maxRetries; attempt++ {
        attemptCtx := logging.AppendCtx(ctx, slog.Int("attempt", attempt))
        
        if err := r.callExternalService(attemptCtx, object); err != nil {
            if attempt == maxRetries {
                ctlrutils.LogError(attemptCtx, r.Logger, "External service call failed after all retries", err,
                    slog.Int("maxRetries", maxRetries),
                    slog.String("serviceEndpoint", r.ServiceURL))
                return err
            }
            
            r.Logger.WarnContext(attemptCtx, "External service call failed, retrying",
                slog.String("error", err.Error()),
                slog.Duration("retryDelay", time.Duration(attempt)*time.Second))
            
            time.Sleep(time.Duration(attempt) * time.Second)
            continue
        }
        
        r.Logger.InfoContext(attemptCtx, "External service call succeeded")
        break
    }
    
    // 3. Resource Conflict Error
    if err := r.createResource(ctx, object); err != nil {
        if errors.IsAlreadyExists(err) {
            r.Logger.WarnContext(ctx, "Resource already exists, updating instead",
                slog.String("resourceType", "ConfigMap"),
                slog.String("name", object.Name))
            
            return r.updateResource(ctx, object)
        }
        
        ctlrutils.LogError(ctx, r.Logger, "Failed to create resource", err,
            slog.String("resourceType", "ConfigMap"),
            slog.String("operation", "create"))
        return err
    }
    
    // 4. Timeout Error with Duration Context
    timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    start := time.Now()
    if err := r.waitForCondition(timeoutCtx, object); err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            ctlrutils.LogError(ctx, r.Logger, "Operation timed out", err,
                slog.Duration("timeout", 30*time.Second),
                slog.Duration("elapsed", time.Since(start)),
                slog.String("condition", "ResourceReady"))
        } else {
            ctlrutils.LogError(ctx, r.Logger, "Failed to wait for condition", err)
        }
        return err
    }
    
    return nil
}
```

**Expected Error Log Output:**

```json
{"time":"2025-08-14T15:23:36Z","level":"ERROR","msg":"Resource validation failed","resource":"MyResource","name":"test","namespace":"default","error":"invalid field 'spec.type': must not be empty","validationType":"schema","resourceSpec":""}
{"time":"2025-08-14T15:23:37Z","level":"WARN","msg":"External service call failed, retrying","resource":"MyResource","name":"test","namespace":"default","attempt":1,"error":"connection refused","retryDelay":"1s"}
{"time":"2025-08-14T15:23:38Z","level":"WARN","msg":"External service call failed, retrying","resource":"MyResource","name":"test","namespace":"default","attempt":2,"error":"connection refused","retryDelay":"2s"}
{"time":"2025-08-14T15:23:41Z","level":"ERROR","msg":"External service call failed after all retries","resource":"MyResource","name":"test","namespace":"default","attempt":3,"error":"connection refused","maxRetries":3,"serviceEndpoint":"https://api.example.com"}
{"time":"2025-08-14T15:23:45Z","level":"ERROR","msg":"Operation timed out","resource":"MyResource","name":"test","namespace":"default","error":"context deadline exceeded","timeout":"30s","elapsed":"30.1s","condition":"ResourceReady"}
```

## Performance Monitoring

### Detailed Performance Tracking

```go
func (r *PerformanceReconciler) trackPerformance(ctx context.Context, object *MyResource) error {
    overallStart := time.Now()
    
    // Track database operations
    dbStart := time.Now()
    ctx = ctlrutils.LogOperation(ctx, r.Logger, "database_query", "Querying resource dependencies")
    
    dependencies, err := r.queryDependencies(ctx, object)
    if err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Database query failed", err)
        return err
    }
    
    dbDuration := time.Since(dbStart)
    r.Logger.InfoContext(ctx, "Database query completed",
        slog.Duration("dbQueryDuration", dbDuration),
        slog.Int("dependencyCount", len(dependencies)),
        slog.String("operation", "database_query"))
    
    // Track API calls
    apiStart := time.Now()
    ctx = ctlrutils.LogOperation(ctx, r.Logger, "api_calls", "Making external API calls")
    
    var apiCallCount int
    for _, dep := range dependencies {
        callStart := time.Now()
        
        if err := r.callExternalAPI(ctx, dep); err != nil {
            ctlrutils.LogError(ctx, r.Logger, "External API call failed", err,
                slog.String("dependency", dep.Name),
                slog.Duration("callDuration", time.Since(callStart)))
            continue
        }
        
        apiCallCount++
        r.Logger.InfoContext(ctx, "API call completed",
            slog.String("dependency", dep.Name),
            slog.Duration("callDuration", time.Since(callStart)))
    }
    
    apiDuration := time.Since(apiStart)
    r.Logger.InfoContext(ctx, "All API calls completed",
        slog.Duration("totalAPIDuration", apiDuration),
        slog.Int("successfulCalls", apiCallCount),
        slog.Int("totalCalls", len(dependencies)),
        slog.Float64("successRate", float64(apiCallCount)/float64(len(dependencies))))
    
    // Track resource creation
    createStart := time.Now()
    ctx = ctlrutils.LogOperation(ctx, r.Logger, "resource_creation", "Creating Kubernetes resources")
    
    var createdResources []string
    for _, dep := range dependencies {
        resourceStart := time.Now()
        
        resource, err := r.createKubernetesResource(ctx, dep)
        if err != nil {
            ctlrutils.LogError(ctx, r.Logger, "Resource creation failed", err,
                slog.String("dependency", dep.Name),
                slog.Duration("resourceCreationDuration", time.Since(resourceStart)))
            continue
        }
        
        createdResources = append(createdResources, resource.Name)
        r.Logger.InfoContext(ctx, "Resource created",
            slog.String("resourceName", resource.Name),
            slog.String("resourceType", resource.Kind),
            slog.Duration("resourceCreationDuration", time.Since(resourceStart)))
    }
    
    createDuration := time.Since(createStart)
    overallDuration := time.Since(overallStart)
    
    // Performance summary
    r.Logger.InfoContext(ctx, "Performance summary",
        slog.Duration("overallDuration", overallDuration),
        slog.Duration("dbQueryDuration", dbDuration),
        slog.Duration("apiCallsDuration", apiDuration),
        slog.Duration("resourceCreationDuration", createDuration),
        slog.Float64("dbQueryPercent", float64(dbDuration)/float64(overallDuration)*100),
        slog.Float64("apiCallsPercent", float64(apiDuration)/float64(overallDuration)*100),
        slog.Float64("resourceCreationPercent", float64(createDuration)/float64(overallDuration)*100),
        slog.Int("totalDependencies", len(dependencies)),
        slog.Int("createdResources", len(createdResources)))
    
    return nil
}
```

**Expected Performance Log Output:**

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Querying resource dependencies","resource":"MyResource","name":"perf-test","namespace":"default","operation":"database_query"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Database query completed","resource":"MyResource","name":"perf-test","namespace":"default","dbQueryDuration":"1.2s","dependencyCount":5,"operation":"database_query"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Making external API calls","resource":"MyResource","name":"perf-test","namespace":"default","operation":"api_calls"}
{"time":"2025-08-14T15:23:38Z","level":"INFO","msg":"API call completed","resource":"MyResource","name":"perf-test","namespace":"default","dependency":"service-a","callDuration":"0.8s"}
{"time":"2025-08-14T15:23:39Z","level":"INFO","msg":"API call completed","resource":"MyResource","name":"perf-test","namespace":"default","dependency":"service-b","callDuration":"1.1s"}
{"time":"2025-08-14T15:23:40Z","level":"INFO","msg":"All API calls completed","resource":"MyResource","name":"perf-test","namespace":"default","totalAPIDuration":"3.2s","successfulCalls":5,"totalCalls":5,"successRate":1}
{"time":"2025-08-14T15:23:42Z","level":"INFO","msg":"Performance summary","resource":"MyResource","name":"perf-test","namespace":"default","overallDuration":"6.1s","dbQueryDuration":"1.2s","apiCallsDuration":"3.2s","resourceCreationDuration":"1.7s","dbQueryPercent":19.7,"apiCallsPercent":52.5,"resourceCreationPercent":27.9,"totalDependencies":5,"createdResources":5}
```

## Context Enrichment

### Building Rich Context

```go
func (r *ContextReconciler) enrichContext(ctx context.Context, cluster *ClusterResource) context.Context {
    // Add basic cluster information
    ctx = logging.AppendCtx(ctx, slog.String("clusterID", cluster.Spec.ClusterID))
    ctx = logging.AppendCtx(ctx, slog.String("clusterType", cluster.Spec.Type))
    ctx = logging.AppendCtx(ctx, slog.String("region", cluster.Spec.Region))
    
    // Add deployment information
    if cluster.Spec.Deployment != nil {
        ctx = logging.AppendCtx(ctx, slog.String("deploymentStrategy", cluster.Spec.Deployment.Strategy))
        ctx = logging.AppendCtx(ctx, slog.Bool("highAvailability", cluster.Spec.Deployment.HighAvailability))
    }
    
    // Add resource requirements
    if cluster.Spec.Resources != nil {
        ctx = logging.AppendCtx(ctx, slog.Int("cpuCores", cluster.Spec.Resources.CPU))
        ctx = logging.AppendCtx(ctx, slog.String("memory", cluster.Spec.Resources.Memory))
        ctx = logging.AppendCtx(ctx, slog.String("storage", cluster.Spec.Resources.Storage))
    }
    
    // Add networking information
    if cluster.Spec.Network != nil {
        ctx = logging.AppendCtx(ctx, slog.String("networkProvider", cluster.Spec.Network.Provider))
        ctx = logging.AppendCtx(ctx, slog.String("podCIDR", cluster.Spec.Network.PodCIDR))
        ctx = logging.AppendCtx(ctx, slog.String("serviceCIDR", cluster.Spec.Network.ServiceCIDR))
    }
    
    // Add status information if available
    if cluster.Status.Phase != "" {
        ctx = logging.AppendCtx(ctx, slog.String("currentPhase", cluster.Status.Phase))
        ctx = logging.AppendCtx(ctx, slog.Int("readyNodes", cluster.Status.ReadyNodes))
        ctx = logging.AppendCtx(ctx, slog.Int("totalNodes", cluster.Status.TotalNodes))
    }
    
    // Add user/request context
    if userID := cluster.Annotations["user.id"]; userID != "" {
        ctx = logging.AppendCtx(ctx, slog.String("userID", userID))
    }
    
    if requestID := cluster.Annotations["request.id"]; requestID != "" {
        ctx = logging.AppendCtx(ctx, slog.String("requestID", requestID))
    }
    
    return ctx
}

func (r *ContextReconciler) processWithRichContext(ctx context.Context, cluster *ClusterResource) error {
    // Enrich context once
    ctx = r.enrichContext(ctx, cluster)
    
    // All subsequent operations include rich context automatically
    r.Logger.InfoContext(ctx, "Starting cluster processing")
    
    // Add operation-specific context
    ctx = logging.AppendCtx(ctx, slog.String("operation", "validation"))
    
    if err := r.validateCluster(ctx, cluster); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Cluster validation failed", err)
        return err
    }
    
    r.Logger.InfoContext(ctx, "Cluster validation completed")
    
    // Change operation context
    ctx = logging.AppendCtx(ctx, slog.String("operation", "provisioning"))
    
    if err := r.provisionCluster(ctx, cluster); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Cluster provisioning failed", err)
        return err
    }
    
    r.Logger.InfoContext(ctx, "Cluster provisioning completed")
    return nil
}
```

**Expected Rich Context Output:**

```json
{"time":"2025-08-14T15:23:36Z","level":"INFO","msg":"Starting cluster processing","resource":"ClusterResource","name":"prod-cluster","namespace":"clusters","clusterID":"cluster-001","clusterType":"production","region":"us-east-1","deploymentStrategy":"rolling","highAvailability":true,"cpuCores":64,"memory":"256Gi","storage":"2Ti","networkProvider":"calico","podCIDR":"10.244.0.0/16","serviceCIDR":"10.96.0.0/12","currentPhase":"Provisioning","readyNodes":3,"totalNodes":5,"userID":"user-123","requestID":"req-456"}
{"time":"2025-08-14T15:23:37Z","level":"INFO","msg":"Cluster validation completed","resource":"ClusterResource","name":"prod-cluster","namespace":"clusters","clusterID":"cluster-001","clusterType":"production","region":"us-east-1","deploymentStrategy":"rolling","highAvailability":true,"cpuCores":64,"memory":"256Gi","storage":"2Ti","networkProvider":"calico","podCIDR":"10.244.0.0/16","serviceCIDR":"10.96.0.0/12","currentPhase":"Provisioning","readyNodes":3,"totalNodes":5,"userID":"user-123","requestID":"req-456","operation":"validation"}
```

## Integration with Existing Code

### Gradual Migration Example

```go
// Before migration
func (r *LegacyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log.Printf("Processing %s/%s", req.Namespace, req.Name)
    
    object := &MyResource{}
    if err := r.Client.Get(ctx, req.NamespacedName, object); err != nil {
        log.Printf("Error fetching resource: %v", err)
        return ctrl.Result{}, err
    }
    
    log.Printf("Processing resource %s", object.Name)
    
    if err := r.processResource(object); err != nil {
        log.Printf("Error processing resource: %v", err)
        return ctrl.Result{}, err
    }
    
    log.Printf("Resource %s processed successfully", object.Name)
    return ctrl.Result{}, nil
}

// During migration - hybrid approach
func (r *MigratingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    
    // NEW: Add structured logging setup
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "MyResource")
    
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
    
    // LEGACY: Keep existing logic but enhance logging
    object := &MyResource{}
    if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
        // OLD: log.Printf("Error fetching resource: %v", err)
        // NEW: Structured error logging
        ctlrutils.LogError(ctx, r.Logger, "Error fetching resource", err)
        return
    }
    
    // NEW: Add object context
    ctx = ctlrutils.AddObjectContext(ctx, object)
    
    // OLD: log.Printf("Processing resource %s", object.Name)
    // NEW: Context-aware logging
    r.Logger.InfoContext(ctx, "Processing resource")
    
    // LEGACY: Keep existing business logic unchanged
    if err = r.processResource(object); err != nil {
        // OLD: log.Printf("Error processing resource: %v", err)
        // NEW: Structured error logging
        ctlrutils.LogError(ctx, r.Logger, "Error processing resource", err)
        return
    }
    
    // OLD: log.Printf("Resource %s processed successfully", object.Name)
    // NEW: Context-aware success logging
    r.Logger.InfoContext(ctx, "Resource processed successfully")
    
    return
}

// After migration - fully structured
func (r *ModernReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
    startTime := time.Now()
    
    ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "MyResource")
    
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
    
    object := &MyResource{}
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
    
    // Phase-based processing
    ctx = ctlrutils.LogPhaseStart(ctx, r.Logger, "processing")
    phaseStartTime := time.Now()
    
    if err = r.processResource(ctx, object); err != nil {
        ctlrutils.LogError(ctx, r.Logger, "Resource processing failed", err)
        return
    }
    
    ctlrutils.LogPhaseComplete(ctx, r.Logger, "processing", time.Since(phaseStartTime))
    r.Logger.InfoContext(ctx, "Resource processing completed successfully")
    
    return
}
```

This examples document shows the progression from basic logging to our comprehensive structured logging system, making it easy for teams to understand and adopt the patterns incrementally.

---

These examples demonstrate the power and flexibility of the O-Cloud Manager structured logging system. Each pattern can be adapted to specific use cases while maintaining consistency across the entire platform.

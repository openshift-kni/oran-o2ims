/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

/*
Generated-By: Claude/Cursor AI Assistant
*/

package utils

import (
	"context"
	"log/slog"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Standard logging attribute names
const (
	LogAttrResource        = "resource"
	LogAttrNamespace       = "namespace"
	LogAttrResourceVersion = "resourceVersion"
	LogAttrGeneration      = "generation"
	LogAttrError           = "error"
	LogAttrDuration        = "duration"
	LogAttrPhase           = "phase"
	LogAttrAction          = "action"
	LogAttrOperation       = "operation"
)

// LogReconcileStart adds standard reconciliation context and logs start message
func LogReconcileStart(ctx context.Context, logger *slog.Logger, req ctrl.Request, resourceType string) context.Context {
	ctx = logging.AppendCtx(ctx, slog.String(LogAttrResource, resourceType))
	ctx = logging.AppendCtx(ctx, slog.String("name", req.Name))
	ctx = logging.AppendCtx(ctx, slog.String(LogAttrNamespace, req.Namespace))
	ctx = logging.AppendCtx(ctx, slog.String(LogAttrAction, "reconcile_start"))

	logger.InfoContext(ctx, "Starting reconciliation")
	return ctx
}

// AddObjectContext adds standard object metadata to context
func AddObjectContext(ctx context.Context, obj client.Object) context.Context {
	if obj != nil {
		ctx = logging.AppendCtx(ctx, slog.String(LogAttrResourceVersion, obj.GetResourceVersion()))
		ctx = logging.AppendCtx(ctx, slog.Int64(LogAttrGeneration, obj.GetGeneration()))

		// Add any relevant labels or annotations as context
		if labels := obj.GetLabels(); len(labels) > 0 {
			// Add important labels - customize based on your needs
			if clusterID, exists := labels["cluster-id"]; exists {
				ctx = logging.AppendCtx(ctx, slog.String("clusterID", clusterID))
			}
			if hwPlugin, exists := labels["hardware-plugin"]; exists {
				ctx = logging.AppendCtx(ctx, slog.String("hardwarePlugin", hwPlugin))
			}
		}
	}
	return ctx
}

// LogPhaseStart logs the start of a reconciliation phase
func LogPhaseStart(ctx context.Context, logger *slog.Logger, phase string) context.Context {
	ctx = logging.AppendCtx(ctx, slog.String(LogAttrPhase, phase))
	logger.InfoContext(ctx, "Phase started")
	return ctx
}

// LogPhaseComplete logs the completion of a reconciliation phase
func LogPhaseComplete(ctx context.Context, logger *slog.Logger, phase string, duration time.Duration) {
	logger.InfoContext(ctx, "Phase completed",
		slog.String(LogAttrPhase, phase),
		slog.Duration(LogAttrDuration, duration))
}

// LogError provides standardized error logging
func LogError(ctx context.Context, logger *slog.Logger, msg string, err error, attrs ...slog.Attr) {
	// Convert slog.Attr to []any for the logger call
	args := make([]any, 0, len(attrs)*2+2)
	args = append(args, LogAttrError, err.Error())
	for _, attr := range attrs {
		args = append(args, attr.Key, attr.Value)
	}
	logger.ErrorContext(ctx, msg, args...)
}

// LogOperation logs a specific operation with context
func LogOperation(ctx context.Context, logger *slog.Logger, operation, msg string, attrs ...slog.Attr) context.Context {
	ctx = logging.AppendCtx(ctx, slog.String(LogAttrOperation, operation))
	// Convert slog.Attr to []any for the logger call
	args := make([]any, 0, len(attrs)*2+2)
	args = append(args, LogAttrOperation, operation)
	for _, attr := range attrs {
		args = append(args, attr.Key, attr.Value)
	}
	logger.InfoContext(ctx, msg, args...)
	return ctx
}

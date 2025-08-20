/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
)

// PathEvaluatorBuilder contains the logic and data needed to create attribute path evaluators.
// Don't create instances of this type directly, use the NewPathEvaluator function instead.
type PathEvaluatorBuilder struct {
	logger             *slog.Logger
	allowMissingFields bool
}

// PathEvaluator knows how extract from an object the value of an attribute given its path. Don't
// create instances of this type directly use the NewPathEvaluator function instead.
type PathEvaluator struct {
	logger             *slog.Logger
	allowMissingFields bool
}

// NewPathEvaluator creates a builder that can then be used to configure and create path
// evaluators.
func NewPathEvaluator() *PathEvaluatorBuilder {
	return &PathEvaluatorBuilder{}
}

// SetLogger sets the logger that the evaluator will use to write log messages. This is mandatory.
func (b *PathEvaluatorBuilder) SetLogger(value *slog.Logger) *PathEvaluatorBuilder {
	b.logger = value
	return b
}

// SetAllowMissingFields sets whether the evaluator should return nil for missing fields
// instead of errors. This is useful for optional fields in API responses.
func (b *PathEvaluatorBuilder) SetAllowMissingFields(value bool) *PathEvaluatorBuilder {
	b.allowMissingFields = value
	return b
}

// Build uses the configuration stored in the builder to create a new evaluator.
func (b *PathEvaluatorBuilder) Build() (result *PathEvaluator, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create and populate the object:
	result = &PathEvaluator{
		logger:             b.logger,
		allowMissingFields: b.allowMissingFields,
	}
	return
}

// Evaluate receives the attribute path and the object and returns the value of that attribute.
func (r *PathEvaluator) Evaluate(ctx context.Context, path Path, object any) (result any,
	err error) {
	value, err := r.evaluate(ctx, path, reflect.ValueOf(object))
	if err != nil {
		err = fmt.Errorf(
			"failed to evaluate '%s': %w",
			strings.Join(path, "/"), err,
		)
		return
	}
	if !value.IsValid() {
		result = nil
	} else {
		result = value.Interface()
	}
	return
}

func (r *PathEvaluator) evaluate(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	if len(path) == 0 {
		result = object
		return
	}
	kind := object.Kind()
	switch kind {
	case reflect.Struct:
		result, err = r.evaluateStruct(ctx, path, object)
	case reflect.Pointer:
		result, err = r.evaluatePointer(ctx, path, object)
	case reflect.Map:
		result, err = r.evaluateMap(ctx, path, object)
	case reflect.Interface:
		result, err = r.evaluateInterface(ctx, path, object)
	default:
		err = fmt.Errorf(
			"expected struct, slice or map, but found '%s'",
			object.Type(),
		)
	}
	return
}

func (r *PathEvaluator) evaluateStruct(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	field := path[0]
	value := object.FieldByName(field)
	if !value.IsValid() {
		if r.allowMissingFields {
			// Return nil for missing optional fields
			result = reflect.ValueOf(nil)
			return
		}
		typ := object.Type()
		err = fmt.Errorf(
			"struct '%s' from package '%s' doesn't have a '%s' field",
			typ.Name(), typ.PkgPath(), field,
		)
		return
	}
	result, err = r.evaluate(ctx, path[1:], value)
	return
}

func (r *PathEvaluator) evaluatePointer(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	if object.IsNil() {
		result = reflect.ValueOf(nil)
		return
	}
	value := object.Elem()
	result, err = r.evaluate(ctx, path, value)
	return
}

func (r *PathEvaluator) evaluateMap(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	key := path[0]
	value := object.MapIndex(reflect.ValueOf(key))
	if !value.IsValid() {
		if r.allowMissingFields {
			// Return nil for missing optional fields
			result = reflect.ValueOf(nil)
			return
		}
		err = fmt.Errorf("map doesn't have a '%s' key", path[0])
		return
	}
	result, err = r.evaluate(ctx, path[1:], value)
	return
}

func (r *PathEvaluator) evaluateInterface(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	if object.IsNil() {
		result = reflect.ValueOf(nil)
		return
	}
	value := object.Elem()
	result, err = r.evaluate(ctx, path, value)
	return
}

/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// ProjectorEvaluatorBuilder contains the logic and data needed to create field projection
// evaluators. Don't create instances of this type directly, use the NewProjectorEvaluator
// function instead.
type ProjectorEvaluatorBuilder struct {
	logger        *slog.Logger
	pathEvaluator func(context.Context, []string, any) (any, error)
}

// ProjectorEvaluator knows how to evaluate field projection. Don't create instances of this type
// directly, use the NewProjectionEvaluator function instead.
type ProjectorEvaluator struct {
	logger        *slog.Logger
	pathEvaluator func(context.Context, []string, any) (any, error)
}

// NewProjectorEvaluator creates a builder that can then be used to configure and create field
// selector evaluators.
func NewProjectorEvaluator() *ProjectorEvaluatorBuilder {
	return &ProjectorEvaluatorBuilder{}
}

// SetLogger sets the logger that the evaluator will use to write log messages. This is mandatory.
func (b *ProjectorEvaluatorBuilder) SetLogger(value *slog.Logger) *ProjectorEvaluatorBuilder {
	b.logger = value
	return b
}

// SetPathEvaluator sets the function that will be used to extract values of attributes from the
// object. This is mandatory.
//
// The path evaluator function receives the attribute path and the object and should return the
// value of that attribute. For example, for a simple struct like this:
//
//	type Person struct {
//		Name string
//		Age  int
//	}
//
// The path evaluator function could be like this:
//
//	func personPathEvaluator(ctx context.Context, path []string, object any) (result any, err error) {
//		person, ok := object.(*Person)
//		if !ok {
//			err = fmt.Errorf("expected person, but got '%T'", object)
//			return
//		}
//		if len(path) != 1 {
//			err = fmt.Errorf("expected exactly one path segment, but got %d", len(path))
//			return
//		}
//		segment := path[0]
//		switch segment {
//		case "name":
//			result = person.Name
//		case "age":
//			result = person.Age
//		default:
//			err = fmt.Errorf(
//				"unknown attribute '%', valid attributes are 'name' and 'age'",
//				segment,
//			)
//		}
//		return
//
// The path evaluator function should return an error if the object isn't of the expected type, of
// if the path doesn't correspond to a valid attribute.
//
// The path evaluator function should return nil if the path corresponds to a valid optional
// attribute that hasn't a value.
func (b *ProjectorEvaluatorBuilder) SetPathEvaluator(
	value func(context.Context, []string, any) (any, error)) *ProjectorEvaluatorBuilder {
	b.pathEvaluator = value
	return b
}

// Build uses the configuration stored in the builder to create a new evaluator.
func (b *ProjectorEvaluatorBuilder) Build() (result *ProjectorEvaluator, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.pathEvaluator == nil {
		err = errors.New("path evaluator is mandatory")
		return
	}

	// Create and populate the object:
	result = &ProjectorEvaluator{
		logger:        b.logger,
		pathEvaluator: b.pathEvaluator,
	}
	return
}

// Evaluate evaluates the projector on the given object. It returns a map containing only the
// fields that match the projector.
func (e *ProjectorEvaluator) Evaluate(ctx context.Context, selector [][]string,
	object any) (result map[string]any, err error) {
	result, err = e.evaluateProjector(ctx, selector, object)
	if e.logger.Enabled(ctx, slog.LevelDebug) {
		e.logger.Debug(
			"Evaluated projector",
			slog.String("projector", fmt.Sprintf("%v", selector)),
			slog.Any("object", object),
			slog.Any("result", result),
		)
	}
	return
}

func (e *ProjectorEvaluator) evaluateProjector(ctx context.Context, selector [][]string,
	object any) (result map[string]any, err error) {
	result = map[string]any{}
	for _, path := range selector {
		err = e.evaluatePath(ctx, path, object, result)
		if err != nil {
			return
		}
	}
	return
}

func (e *ProjectorEvaluator) evaluatePath(ctx context.Context, path []string, object any, result map[string]any) error {
	value, err := e.pathEvaluator(ctx, path, object)
	if err != nil {
		return err
	}
	return e.setPath(path, result, value)
}

func (e *ProjectorEvaluator) setPath(path []string, object map[string]any, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("path must have at least one element")
	}
	head := path[0]
	tail := path[1:]
	if len(tail) == 0 {
		object[head] = value
		return nil
	}
	next, ok := object[head]
	if ok {
		switch next := next.(type) {
		case map[string]any:
			next[head] = value
			return e.setPath(tail, next, value)
		default:
			empty := map[string]any{}
			object[head] = empty
			return e.setPath(tail, empty, value)
		}
	} else {
		next := map[string]any{}
		object[head] = next
		return e.setPath(tail, next, value)
	}
}

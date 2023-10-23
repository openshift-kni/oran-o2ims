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

package filter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
)

// EvaluatorBuilder contains the logic and data needed to create filter expression evaluators. Don't
// create instances of this type directly, use the NewEvaluator function instead.
type EvaluatorBuilder struct {
	logger   *slog.Logger
	resolver func(context.Context, []string, any) (any, error)
}

// Evaluator knows how to evaluate filter expressions. Don't create instances of this type
// directly, use the NewEvaluator function instead.
type Evaluator struct {
	logger   *slog.Logger
	resolver func(context.Context, []string, any) (any, error)
}

// NewEvaluator creates a builder that can then be used to configure and create expression filter
// evaluators.
func NewEvaluator() *EvaluatorBuilder {
	return &EvaluatorBuilder{}
}

// SetLogger sets the logger that the evaluator will use to write log messages. This is mandatory.
func (b *EvaluatorBuilder) SetLogger(value *slog.Logger) *EvaluatorBuilder {
	b.logger = value
	return b
}

// SetResolver sets the function that will be used to extract values of attributes from the
// object. This is mandatory.
//
// The resolver function receives the attribute path and the object and should return the value of
// that attribute. For example, for a simple struct like this:
//
//	type Person struct {
//		Name string
//		Age  int
//	}
//
// The resolver function could be like this:
//
//	func personResolver(ctx context.Context, path []string, object any) (result any, err error) {
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
// The resolver function should return an error if the object isn't of the expected type, of if the
// path doesn't correspond to a valid attribute.
//
// The resolver function should return nil if the path corresponds to a valid optional attribute
// that hasn't a value.
func (b *EvaluatorBuilder) SetResolver(
	value func(context.Context, []string, any) (any, error)) *EvaluatorBuilder {
	b.resolver = value
	return b
}

// Build uses the configuration stored in the builder to create a new evaluator.
func (b *EvaluatorBuilder) Build() (result *Evaluator, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.resolver == nil {
		err = errors.New("resolver is mandatory")
		return
	}

	// Create and populate the object:
	result = &Evaluator{
		logger:   b.logger,
		resolver: b.resolver,
	}
	return
}

// Evaluate evaluates the filter expression on the given object. It returns true if the object
// matches the expression, and false otherwise.
func (e *Evaluator) Evaluate(ctx context.Context, expr *Expr, object any) (result bool,
	err error) {
	result, err = e.evaluateExpr(ctx, expr, object)
	if e.logger.Enabled(ctx, slog.LevelDebug) {
		e.logger.Debug(
			"Evaluated filter expression",
			"expr", expr.String(),
			"object", object,
			"result", result,
		)
	}
	return
}

func (e *Evaluator) evaluateExpr(ctx context.Context, expr *Expr, object any) (result bool,
	err error) {
	for _, term := range expr.Terms {
		result, err = e.evaluateTerm(ctx, term, object)
		if !result || err != nil {
			return
		}
	}
	result = true
	return
}

func (e *Evaluator) evaluateTerm(ctx context.Context, term *Term, object any) (result bool,
	err error) {
	value, err := e.resolver(ctx, term.Path, object)
	if err != nil {
		return
	}
	switch term.Operator {
	case Cont:
		result, err = e.evaluateCont(value, term.Values)
	case Eq:
		result, err = e.evaluateEq(value, term.Values)
	case Gt:
		result, err = e.evaluateGt(value, term.Values)
	case Gte:
		result, err = e.evaluateGte(value, term.Values)
	case In:
		result, err = e.evaluateIn(value, term.Values)
	case Lt:
		result, err = e.evaluateLt(value, term.Values)
	case Lte:
		result, err = e.evaluateLte(value, term.Values)
	case Ncont:
		result, err = e.evaluateNcont(value, term.Values)
	case Neq:
		result, err = e.evaluateNeq(value, term.Values)
	case Nin:
		result, err = e.evaluateNin(value, term.Values)
	default:
		err = fmt.Errorf("unknown operator %v", term.Operator)
	}
	return
}

func (e *Evaluator) evaluateCont(value any, args []any) (result bool,
	err error) {
	str, ok := value.(string)
	if !ok {
		err = fmt.Errorf(
			"the 'cont' operator requires a string attribute, but got '%T'",
			value,
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	for _, arg := range args {
		arg := arg.(string)
		if strings.Contains(str, arg) {
			result = true
			return
		}
	}
	result = false
	return
}

func (e *Evaluator) evaluateEq(value any, args []any) (result bool,
	err error) {
	if len(args) != 1 {
		err = fmt.Errorf(
			"the 'eq' operator expects exacly one value, but got %d",
			len(args),
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	arg := args[0]
	switch value := value.(type) {
	case string:
		arg := arg.(string)
		result = value == arg
	case int:
		arg := arg.(int)
		result = value == arg
	default:
		err = fmt.Errorf(
			"the 'eq' operator supports attributes containing strings, numbers, "+
				"enumerations and booleans, but got attribute of type '%T'",
			value,
		)
	}
	return
}

func (e *Evaluator) evaluateGt(value any, args []any) (result bool,
	err error) {
	if len(args) != 1 {
		err = fmt.Errorf(
			"the 'gt' operator expects exacly one value, but got %d",
			len(args),
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	arg := args[0]
	switch value := value.(type) {
	case string:
		arg := arg.(string)
		result = strings.Compare(value, arg) > 0
	case int:
		arg := arg.(int)
		result = value > arg
	default:
		err = fmt.Errorf(
			"the 'gt' operator supports attributes containing strings, numbers, "+
				"booleans and dates, but got attribute of type %T",
			value,
		)
	}
	return
}

func (e *Evaluator) evaluateGte(value any, args []any) (result bool,
	err error) {
	if len(args) != 1 {
		err = fmt.Errorf(
			"the 'gte' operator expects exacly one value, but got %d",
			len(args),
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	arg := args[0]
	switch value := value.(type) {
	case string:
		arg := arg.(string)
		result = strings.Compare(value, arg) >= 0
	case int:
		arg := arg.(int)
		result = value >= arg
	default:
		err = fmt.Errorf(
			"the 'gte' operator supports attributes containing strings, numbers, "+
				"booleans and dates, but got attribute of type %T",
			value,
		)
	}
	return
}

func (e *Evaluator) evaluateIn(value any, args []any) (result bool,
	err error) {
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	for _, arg := range args {
		if reflect.DeepEqual(value, arg) {
			result = true
			return
		}
	}
	result = false
	return
}

func (e *Evaluator) evaluateLt(value any, args []any) (result bool,
	err error) {
	if len(args) != 1 {
		err = fmt.Errorf(
			"the 'lt' operator expects exacly one value, but got %d",
			len(args),
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	arg := args[0]
	switch value := value.(type) {
	case string:
		arg := arg.(string)
		result = strings.Compare(value, arg) < 0
	case int:
		arg := arg.(int)
		result = value < arg
	default:
		err = fmt.Errorf(
			"the 'lt' operator supports attributes containing strings, numbers, "+
				"booleans and dates, but got attribute of type '%T'",
			value,
		)
	}
	return
}

func (e *Evaluator) evaluateLte(value any, args []any) (result bool,
	err error) {
	if len(args) != 1 {
		err = fmt.Errorf(
			"the 'lte' operator expects exacly one value, but got %d",
			len(args),
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	arg := args[0]
	switch value := value.(type) {
	case string:
		arg := arg.(string)
		result = strings.Compare(value, arg) <= 0
	case int:
		arg := arg.(int)
		result = value <= arg
	default:
		err = fmt.Errorf(
			"the 'lt' operator supports attributes containing strings, numbers, "+
				"booleans and dates, but got attribute of type '%T'",
			value,
		)
	}
	return
}

func (e *Evaluator) evaluateNcont(value any, args []any) (result bool,
	err error) {
	str, ok := value.(string)
	if !ok {
		err = fmt.Errorf(
			"the 'ncont' operator requires a string attribute, but got %T",
			value,
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	for _, arg := range args {
		arg := arg.(string)
		if strings.Contains(str, arg) {
			result = false
			return
		}
	}
	result = true
	return
}

func (e *Evaluator) evaluateNeq(value any, args []any) (result bool,
	err error) {
	if len(args) != 1 {
		err = fmt.Errorf(
			"the 'neq' operator expects exacly one value, but got %d",
			len(args),
		)
		return
	}
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	arg := args[0]
	switch value := value.(type) {
	case string:
		arg := arg.(string)
		result = value != arg
	case int:
		arg := arg.(int)
		result = value != arg
	default:
		err = fmt.Errorf(
			"the 'neq' operator supports attributes containing strings, numbers, "+
				"enumerations and booleans, but got attribute of type '%T'",
			value,
		)
	}
	return
}

func (e *Evaluator) evaluateNin(value any, args []any) (result bool,
	err error) {
	args, err = e.convertArgs(value, args)
	if err != nil {
		return
	}
	for _, arg := range args {
		if reflect.DeepEqual(value, arg) {
			result = false
			return
		}
	}
	result = true
	return
}

func (e *Evaluator) convertArgs(value any, args []any) (result []any, err error) {
	result = make([]any, len(args))
	switch value.(type) {
	case string:
		result, err = e.convertStrings(args)
	case int:
		result, err = e.convertInts(args)
	default:
		err = fmt.Errorf(
			"don't know how to convert values to type %T",
			value,
		)
	}
	return
}

func (e *Evaluator) convertStrings(args []any) (result []any, err error) {
	converted := make([]any, len(args))
	for i, arg := range args {
		switch arg := arg.(type) {
		case string:
			converted[i] = arg
		case int:
			converted[i] = strconv.Itoa(arg)
		default:
			err = fmt.Errorf(
				"don't know how to convert value of type %T to string",
				arg,
			)
			return
		}
	}
	result = converted
	return
}

func (e *Evaluator) convertInts(args []any) (result []any, err error) {
	converted := make([]any, len(args))
	for i, arg := range args {
		switch arg := arg.(type) {
		case string:
			var value int
			value, err = strconv.Atoi(arg)
			if err != nil {
				return
			}
			converted[i] = value
		case int:
			converted[i] = arg
		default:
			err = fmt.Errorf(
				"don't know how to convert value of type %T to integer",
				arg,
			)
			return
		}
	}
	result = converted
	return
}

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

package data

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/jhernand/o2ims/internal/filter"
)

func Filter(expr *filter.Expr, object Object) (result bool, err error) {
	for _, term := range expr.Terms {
		result, err = evalFilterTerm(term, object)
		if !result || err != nil {
			return
		}
	}
	result = true
	return
}

func evalFilterTerm(term *filter.Term, object Object) (result bool, err error) {
	attr, err := evalFilterPath(term.Path, object)
	if err != nil {
		return
	}
	result, err = evalFilterOperator(term.Operator, attr, term.Values)
	return
}

func evalFilterPath(path []string, object Object) (result any, err error) {
	if len(path) != 1 {
		err = fmt.Errorf(
			"only paths with exactly one segment are currently supported, but '%s' "+
				"has %d",
			strings.Join(path, "/"), len(path),
		)
	}
	field := path[0]
	result, ok := object[field]
	if !ok {
		err = fmt.Errorf(
			"object doesn't contain field '%s'",
			field,
		)
		return
	}
	return
}

func evalFilterOperator(operator filter.Operator, attr any, args []any) (result bool, err error) {
	switch operator {
	case filter.Cont:
		attr := attr.(string)
		for _, arg := range args {
			if strings.Contains(attr, arg.(string)) {
				result = true
				return
			}
		}
	case filter.Eq:
		result = reflect.DeepEqual(attr, args[0])
	case filter.Gt:
	case filter.Gte:
	case filter.Lt:
	case filter.In:
	case filter.Lte:
	case filter.Ncont:
	case filter.Neq:
	case filter.Nin:
	default:
		err = fmt.Errorf("unknown filter operator %v", operator)
	}
	return
}

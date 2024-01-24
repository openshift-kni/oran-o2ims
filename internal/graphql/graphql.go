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

package graphql

import (
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/model"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

type FilterOperator search.Operator

// String generates a GraphQL string representation of the operator. It panics if used on an unknown
// operator.
func (o FilterOperator) String() (result string, err error) {
	switch search.Operator(o) {
	case search.Eq:
		result = "="
	case search.Neq:
		result = "!="
	case search.Gt:
		result = ">"
	case search.Gte:
		result = ">="
	case search.Lt:
		result = "<"
	case search.Lte:
		result = "<="
	default:
		err = fmt.Errorf("unknown operator %d", o)
	}
	return
}

type PropertyCluster string

// MapProperty maps a specified O2 property name to the search API property name
func (p PropertyCluster) MapProperty() string {
	switch p {
	case "name":
		return "name"
	case "resourcePoolID":
		return "cluster"
	default:
		// unknown property
		return ""
	}
}

type PropertyNode string

// MapProperty maps a specified O2 property name to the search API property name
func (p PropertyNode) MapProperty() string {
	switch p {
	case "description":
		return "name"
	case "resourcePoolID":
		return "cluster"
	case "globalAssetID":
		return "_uid"
	case "resourceID":
		return "_systemUUID"
	default:
		// unknown property
		return ""
	}
}

type FilterTerm search.Term

// Map a filter term to a GraphQL SearchFilter
func (t FilterTerm) MapFilter(mapPropertyFunc func(string) string) (searchFilter *model.SearchFilter, err error) {
	// Get filter operator
	var operator string
	operator, err = FilterOperator(t.Operator).String()
	if err != nil {
		return
	}

	// Generate values
	values := []*string{}
	for _, v := range t.Values {
		value := fmt.Sprintf("%s%s", operator, v.(string))
		values = append(values, &value)
	}

	// Convert to GraphQL property
	searchProperty := mapPropertyFunc(t.Path[0])

	// Build search filter
	searchFilter = &model.SearchFilter{
		Property: searchProperty,
		Values:   values,
	}

	return
}

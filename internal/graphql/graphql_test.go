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
	"errors"

	"github.com/openshift-kni/oran-o2ims/internal/model"
	"github.com/openshift-kni/oran-o2ims/internal/search"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("GraphQL filters", func() {
	DescribeTable(
		"Map a filter term to a SearchFilter",
		func(term search.Term, expectedFilter *model.SearchFilter, expectedErr error, mapPropertyFunc func(string) string) {
			actual, err := FilterTerm(term).MapFilter(mapPropertyFunc)
			Expect(actual).To(Equal(expectedFilter))
			if expectedErr != nil {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry(
			"Filter term for Cluster",
			search.Term{
				Operator: search.Eq,
				Path: []string{
					"resourcePoolId",
				},
				Values: []any{
					"spoke0",
				},
			},
			&model.SearchFilter{
				Property: "cluster",
				Values:   []*string{ptr.To("=spoke0")},
			},
			nil,
			func(s string) string {
				return PropertyCluster(s).MapProperty()
			},
		),
		Entry(
			"Filter term for Node",
			search.Term{
				Operator: search.Eq,
				Path: []string{
					"resourcePoolId",
				},
				Values: []any{
					"spoke0",
				},
			},
			&model.SearchFilter{
				Property: "cluster",
				Values:   []*string{ptr.To("=spoke0")},
			},
			nil,
			func(s string) string {
				return PropertyNode(s).MapProperty()
			},
		),
		Entry(
			"Fail when an unknown property is specified",
			search.Term{
				Operator: search.Eq,
				Path: []string{
					"location",
				},
				Values: []any{
					"EU",
				},
			},
			nil,
			errors.New("unknown GraphQL property"),
			func(s string) string {
				return PropertyNode(s).MapProperty()
			},
		),
	)
})

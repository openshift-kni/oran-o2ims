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
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Expression", func() {
	DescribeTable(
		"String representation",
		func(expr *Expr, expected string) {
			actual := expr.String()
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Single term",
			&Expr{
				Terms: []*Term{
					{
						Operator: Eq,
						Path: []string{
							"myattr",
						},
						Values: []any{
							"myvalue",
						},
					},
				},
			},
			"(eq,myattr,'myvalue')",
		),
		Entry(
			"Escape ~ in path segment",
			&Expr{
				Terms: []*Term{
					{
						Operator: Eq,
						Path: []string{
							"my~attr",
						},
						Values: []any{
							"myvalue",
						},
					},
				},
			},
			"(eq,my~0attr,'myvalue')",
		),
		Entry(
			"Escape / in path segment",
			&Expr{
				Terms: []*Term{
					{
						Operator: Eq,
						Path: []string{
							"my/attr",
						},
						Values: []any{
							"myvalue",
						},
					},
				},
			},
			"(eq,my~1attr,'myvalue')",
		),
		Entry(
			"Escape @ in path segment",
			&Expr{
				Terms: []*Term{
					{
						Operator: Eq,
						Path: []string{
							"my@attr",
						},
						Values: []any{
							"myvalue",
						},
					},
				},
			},
			"(eq,my~battr,'myvalue')",
		),
		Entry(
			"Dont't escape @ in @key",
			&Expr{
				Terms: []*Term{
					{
						Operator: Eq,
						Path: []string{
							"mydict",
							"@key",
						},
						Values: []any{
							"myvalue",
						},
					},
				},
			},
			"(eq,mydict/@key,'myvalue')",
		),
		Entry(
			"Multiple terms",
			&Expr{
				Terms: []*Term{
					{
						Operator: Eq,
						Path: []string{
							"myattr",
						},
						Values: []any{
							"myvalue",
						},
					},
					{
						Operator: Neq,
						Path: []string{
							"yourattr",
						},
						Values: []any{
							"yourvalue",
						},
					},
				},
			},
			"(eq,myattr,'myvalue');(neq,yourattr,'yourvalue')",
		),
		Entry(
			"Multiple path segments",
			&Expr{
				Terms: []*Term{
					{
						Operator: Eq,
						Path: []string{
							"myattr",
							"yourattr",
						},
						Values: []any{
							"yourvalue",
						},
					},
				},
			},
			"(eq,myattr/yourattr,'yourvalue')",
		),
		Entry(
			"Multiple values",
			&Expr{
				Terms: []*Term{
					{
						Operator: In,
						Path: []string{
							"myattr",
						},
						Values: []any{
							"myvalue",
							"yourvalue",
						},
					},
				},
			},
			"(in,myattr,'myvalue','yourvalue')",
		),
	)
})

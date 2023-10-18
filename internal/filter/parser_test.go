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

var _ = Describe("Parser", func() {
	DescribeTable(
		"Parses correctly",
		func(text string, expected *Expr) {
			parser, err := NewParser().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			actual, err := parser.Parse(text)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Simple equal",
			"(eq,myattr,myvalue)",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"myvalue",
					},
				}},
			},
		),
		Entry(
			"Simple contains",
			"(cont,myattr,myvalue)",
			&Expr{
				Terms: []*Term{{
					Operator: Cont,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"myvalue",
					},
				}},
			},
		),
		Entry(
			"Quoted value with space",
			"(eq,myattr,'my value')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my value",
					},
				}},
			},
		),
		Entry(
			"Quoted value with multiple spaces",
			"(eq,myattr,'my  value')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my  value",
					},
				}},
			},
		),
		Entry(
			"Quoted empty value",
			"(eq,myattr,'')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"",
					},
				}},
			},
		),
		Entry(
			"Quoted right parenthesis",
			"(eq,myattr,'my ) value')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my ) value",
					},
				}},
			},
		),
		Entry(
			"Quoted right parenthesis with multiple spaces",
			"(eq,myattr,'my  )  value')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my  )  value",
					},
				}},
			},
		),
		Entry(
			"Quoted comma",
			"(eq,myattr,'my , value')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my , value",
					},
				}},
			},
		),
		Entry(
			"Quoted comma with multiple spaces",
			"(eq,myattr,'my  ,  value')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my  ,  value",
					},
				}},
			},
		),
		Entry(
			"Quoted single quote",
			"(eq,myattr,'my''value')",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my'value",
					},
				}},
			},
		),
		Entry(
			"Multiple unquoted values",
			"(eq,myattr,my,value)",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my",
						"value",
					},
				}},
			},
		),
		Entry(
			"No values",
			"(eq,myattr,)",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{},
				}},
			},
		),
		Entry(
			"Unquoted value with one space",
			"(eq,myattr, )",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						" ",
					},
				}},
			},
		),
		Entry(
			"Unquoted value with two spaces",
			"(eq,myattr,  )",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"  ",
					},
				}},
			},
		),
		Entry(
			"Quoted value sorrounded by spaces",
			"(eq,myattr,  'my value'  )",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
					},
					Values: []any{
						"my value",
					},
				}},
			},
		),
		Entry(
			"Two path segments",
			"(eq,myattr/yourattr,yourvalue)",
			&Expr{
				Terms: []*Term{{
					Operator: Eq,
					Path: []string{
						"myattr",
						"yourattr",
					},
					Values: []any{
						"yourvalue",
					},
				}},
			},
		),
		Entry(
			"Multiple",
			"(eq,myattr,myvalue);(neq,yourattr,yourvalue)",
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
		),
		Entry(
			"Quoted ~ in path",
			"(eq,my~0attr,myvalue)",
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
		),
		Entry(
			"Quoted / in path",
			"(eq,my~1attr,myvalue)",
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
		),
		Entry(
			"Quoted @ in path",
			"(eq,my~battr,myvalue)",
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
		),
	)
})

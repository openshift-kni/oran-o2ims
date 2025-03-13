/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package search

import (
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Selector parser", func() {
	DescribeTable(
		"Parses correctly",
		func(text string, expected *Selector) {
			parser, err := NewSelectorParser().
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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
			&Selector{
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

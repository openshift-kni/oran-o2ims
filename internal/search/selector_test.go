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

var _ = Describe("Selector", func() {
	DescribeTable(
		"String representation",
		func(selector *Selector, expected string) {
			actual := selector.String()
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Single term",
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
				},
			},
			"(eq,myattr,'myvalue')",
		),
		Entry(
			"Escape ~ in path segment",
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
			"(eq,my~0attr,'myvalue')",
		),
		Entry(
			"Escape / in path segment",
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
			"(eq,my~1attr,'myvalue')",
		),
		Entry(
			"Escape @ in path segment",
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
			"(eq,my~battr,'myvalue')",
		),
		Entry(
			"Dont't escape @ in @key",
			&Selector{
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
			"(eq,myattr,'myvalue');(neq,yourattr,'yourvalue')",
		),
		Entry(
			"Multiple path segments",
			&Selector{
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
			&Selector{
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

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

var _ = Describe("Paths parser", func() {
	DescribeTable(
		"Parses correctly",
		func(text string, expected []Path) {
			parser, err := NewPathsParser().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			actual, err := parser.Parse(text)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"One segment",
			"myattr",
			[]Path{
				{"myattr"},
			},
		),
		Entry(
			"Two segments",
			"myattr/yourattr",
			[]Path{
				{"myattr", "yourattr"},
			},
		),
		Entry(
			"Quoted ~ in path",
			"my~0attr",
			[]Path{
				{"my~attr"},
			},
		),
		Entry(
			"Quoted slash in path",
			"my~1attr",
			[]Path{
				{"my/attr"},
			},
		),
		Entry(
			"Quoted comma in path",
			"my~aattr",
			[]Path{
				{"my,attr"},
			},
		),
		Entry(
			"Multiple paths",
			"myattr,yourattr",
			[]Path{
				{"myattr"},
				{"yourattr"},
			},
		),
		Entry(
			"Multiple paths with multiple segments",
			"myattr/yourattr,myfield/yourfield",
			[]Path{
				{"myattr", "yourattr"},
				{"myfield", "yourfield"},
			},
		),
	)

	It("Aggreates multiple paths", func() {
		parser, err := NewPathsParser().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())
		paths, err := parser.Parse("myfield", "yourfield")
		Expect(err).ToNot(HaveOccurred())
		Expect(paths).To(Equal([]Path{
			{"myfield"},
			{"yourfield"},
		}))
	})
})

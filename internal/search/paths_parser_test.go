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

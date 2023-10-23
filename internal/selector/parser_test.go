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

package selector

import (
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parser", func() {
	DescribeTable(
		"Parses correctly",
		func(text string, expected [][]string) {
			parser, err := NewParser().
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
			[][]string{
				{"myattr"},
			},
		),
		Entry(
			"Two segments",
			"myattr/yourattr",
			[][]string{
				{"myattr", "yourattr"},
			},
		),
		Entry(
			"Quoted ~ in path",
			"my~0attr",
			[][]string{
				{"my~attr"},
			},
		),
		Entry(
			"Quoted slash in path",
			"my~1attr",
			[][]string{
				{"my/attr"},
			},
		),
		Entry(
			"Quoted comma in path",
			"my~aattr",
			[][]string{
				{"my,attr"},
			},
		),
		Entry(
			"Multiple paths",
			"myattr,yourattr",
			[][]string{
				{"myattr"},
				{"yourattr"},
			},
		),
		Entry(
			"Multiple paths with multiple segments",
			"myattr/yourattr,myfield/yourfield",
			[][]string{
				{"myattr", "yourattr"},
				{"myfield", "yourfield"},
			},
		),
	)
})

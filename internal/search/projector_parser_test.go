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

var _ = Describe("Projector parser", func() {
	DescribeTable(
		"Parses correctly",
		func(include, exclude string, expected *Projector) {
			parser, err := NewProjectorParser().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			actual, err := parser.Parse(include, exclude)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Include one segment",
			"myattr",
			"",
			&Projector{
				Include: []Path{
					{"myattr"},
				},
				Exclude: nil,
			},
		),
		Entry(
			"Include two segments",
			"myattr/yourattr",
			"",
			&Projector{
				Include: []Path{
					{"myattr", "yourattr"},
				},
				Exclude: nil,
			},
		),
		Entry(
			"Include quoted ~ in path",
			"my~0attr",
			"",
			&Projector{
				Include: []Path{
					{"my~attr"},
				},
				Exclude: nil,
			},
		),
		Entry(
			"Include quoted slash in path",
			"my~1attr",
			"",
			&Projector{
				Include: []Path{
					{"my/attr"},
				},
				Exclude: nil,
			},
		),
		Entry(
			"Include quoted comma in path",
			"my~aattr",
			"",
			&Projector{
				Include: []Path{
					{"my,attr"},
				},
				Exclude: nil,
			},
		),
		Entry(
			"Include multiple paths",
			"myattr,yourattr",
			"",
			&Projector{
				Include: []Path{
					{"myattr"},
					{"yourattr"},
				},
				Exclude: nil,
			},
		),
		Entry(
			"Include multiple paths with multiple segments",
			"myattr/yourattr,myfield/yourfield",
			"",
			&Projector{
				Include: []Path{
					{"myattr", "yourattr"},
					{"myfield", "yourfield"},
				},
				Exclude: nil,
			},
		),
		Entry(
			"Exclude one segment",
			"",
			"myattr",
			&Projector{
				Include: nil,
				Exclude: []Path{
					{"myattr"},
				},
			},
		),
		Entry(
			"Exclude two segments",
			"",
			"myattr/yourattr",
			&Projector{
				Include: nil,
				Exclude: []Path{
					{"myattr", "yourattr"},
				},
			},
		),
		Entry(
			"Include one field and exclude another",
			"myattr",
			"yourattr",
			&Projector{
				Include: []Path{
					{"myattr"},
				},
				Exclude: []Path{
					{"yourattr"},
				},
			},
		),
	)
})

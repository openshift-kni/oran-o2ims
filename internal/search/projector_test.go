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

var _ = Describe("Projector", func() {
	DescribeTable(
		"Check empty",
		func(projector *Projector, expected bool) {
			Expect(projector.Empty()).To(Equal(expected))
		},
		Entry(
			"Nil",
			nil,
			true,
		),
		Entry(
			"Nil include and exclude",
			&Projector{
				Include: nil,
				Exclude: nil,
			},
			true,
		),
		Entry(
			"Empty include and exclude",
			&Projector{
				Include: []Path{},
				Exclude: []Path{},
			},
			true,
		),
		Entry(
			"Non empty include",
			&Projector{
				Include: []Path{
					{"myattr"},
				},
				Exclude: nil,
			},
			false,
		),
		Entry(
			"Non empty exclude",
			&Projector{
				Include: nil,
				Exclude: []Path{
					{"myattr"},
				},
			},
			false,
		),
		Entry(
			"Non empty include and exclude",
			&Projector{
				Include: []Path{
					{"myattr"},
				},
				Exclude: []Path{
					{"myattr"},
				},
			},
			false,
		),
	)
})

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
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Resolver", func() {
	Describe("Creation", func() {
		It("Can be created with a logger", func() {
			resolver, err := NewResolver().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(resolver).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			resolver, err := NewResolver().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(resolver).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	DescribeTable(
		"Resolves correctly",
		func(path []string, producer func() any, expected any) {
			resolver, err := NewResolver().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			result, err := resolver.Resolve(context.Background(), path, producer())
			Expect(err).ToNot(HaveOccurred())
			if expected == nil {
				Expect(result).To(BeNil())
			} else {
				Expect(result).To(Equal(expected))
			}
		},
		Entry(
			"Integer struct field",
			[]string{"MyField"},
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			123,
		),
		Entry(
			"String struct field",
			[]string{"MyField"},
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			"myvalue",
		),
		Entry(
			"Struct pointer",
			[]string{"MyField"},
			func() any {
				type MyObject struct {
					MyField int
				}
				return &MyObject{
					MyField: 123,
				}
			},
			123,
		),
		Entry(
			"Nil pointer",
			[]string{"MyField"},
			func() any {
				type MyObject struct {
					MyField int
				}
				return (*MyObject)(nil)
			},
			nil,
		),
		Entry(
			"String map entry",
			[]string{"mykey"},
			func() any {
				return map[string]any{
					"mykey": "myvalue",
				}
			},
			"myvalue",
		),
		Entry(
			"Nested struct field",
			[]string{"MyField", "YourField"},
			func() any {
				type YourObject struct {
					YourField string
				}
				type MyObject struct {
					MyField YourObject
				}
				return MyObject{
					YourObject{
						YourField: "yourvalue",
					},
				}
			},
			"yourvalue",
		),
		Entry(
			"Nested map key",
			[]string{"mykey", "yourkey"},
			func() any {
				return map[string]any{
					"mykey": map[string]any{
						"yourkey": "yourvalue",
					},
				}
			},
			"yourvalue",
		),
	)

	DescribeTable(
		"Reports errors",
		func(path []string, producer func() any, expected string) {
			resolver, err := NewResolver().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			_, err = resolver.Resolve(context.Background(), path, producer())
			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).To(MatchRegexp(expected))
		},
		Entry(
			"Struct field that doesn't exist",
			[]string{"MyField"},
			func() any {
				type MyObject struct{}
				return MyObject{}
			},
			"failed to resolve 'MyField': struct 'MyObject' from package '.*' doesn't have a 'MyField' field",
		),
		Entry(
			"Map key that doesn't exist",
			[]string{"mykey"},
			func() any {
				return map[string]any{}
			},
			"failed to resolve 'mykey': map doesn't have a 'mykey' key",
		),
	)
})

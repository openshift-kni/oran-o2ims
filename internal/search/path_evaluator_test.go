/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package search

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Path evaluator", func() {
	Describe("Creation", func() {
		It("Can be created with a logger", func() {
			evaluator, err := NewPathEvaluator().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(evaluator).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			evaluator, err := NewPathEvaluator().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(evaluator).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	DescribeTable(
		"Evaluates correctly",
		func(path []string, producer func() any, expected any) {
			evaluator, err := NewPathEvaluator().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			result, err := evaluator.Evaluate(context.Background(), path, producer())
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
			evaluator, err := NewPathEvaluator().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			_, err = evaluator.Evaluate(context.Background(), path, producer())
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
			"failed to evaluate 'MyField': struct 'MyObject' from package '.*' doesn't have a 'MyField' field",
		),
		Entry(
			"Map key that doesn't exist",
			[]string{"mykey"},
			func() any {
				return map[string]any{}
			},
			"failed to evaluate 'mykey': map doesn't have a 'mykey' key",
		),
	)
})

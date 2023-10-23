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
	"context"

	"github.com/jhernand/o2ims/internal/filter"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Evaluator", func() {
	// nop is a simple resolver that always return nil.
	var nop = func(context.Context, []string, any) (any, error) {
		return nil, nil
	}

	Describe("Creation", func() {
		It("Can be created with a logger and a resolver", func() {
			evaluator, err := NewEvaluator().
				SetLogger(logger).
				SetResolver(nop).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(evaluator).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			evaluator, err := NewEvaluator().
				SetResolver(nop).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(evaluator).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a resolver", func() {
			evaluator, err := NewEvaluator().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(evaluator).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("resolver"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	DescribeTable(
		"Evaluates correctly",
		func(src string, producer func() any, expected map[string]any) {
			// Parse the expression:
			parser, err := NewParser().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			expr, err := parser.Parse(src)
			Expect(err).ToNot(HaveOccurred())

			// Evaluate the expression:
			resolver, err := filter.NewResolver().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			evaluator, err := NewEvaluator().
				SetLogger(logger).
				SetResolver(resolver.Resolve).
				Build()
			Expect(err).ToNot(HaveOccurred())
			actual, err := evaluator.Evaluate(context.Background(), expr, producer())
			Expect(err).ToNot(HaveOccurred())

			// Check the results:
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Top level string",
			"MyField",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			map[string]any{
				"MyField": "myvalue",
			},
		),
		Entry(
			"Nested string",
			"MyField/YourField",
			func() any {
				type YourObject struct {
					YourField string
				}
				type MyObject struct {
					MyField YourObject
				}
				return MyObject{
					MyField: YourObject{
						YourField: "yourvalue",
					},
				}
			},
			map[string]any{
				"MyField": map[string]any{
					"YourField": "yourvalue",
				},
			},
		),
	)
})

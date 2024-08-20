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
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Projector evaluator", func() {
	// nop is a simple path evaluator that always return nil.
	var nop = func(context.Context, Path, any) (any, error) {
		return nil, nil
	}

	Describe("Creation", func() {
		It("Can be created with a logger and a path evaluator", func() {
			evaluator, err := NewProjectorEvaluator().
				SetLogger(logger).
				SetPathEvaluator(nop).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(evaluator).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			evaluator, err := NewProjectorEvaluator().
				SetPathEvaluator(nop).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(evaluator).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a path evaluator", func() {
			evaluator, err := NewProjectorEvaluator().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(evaluator).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("path"))
			Expect(msg).To(ContainSubstring("evaluator"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	DescribeTable(
		"Evaluates correctly",
		func(include, exclude string, producer func() any, expected map[string]any) {
			// Parse the projector:
			parser, err := NewProjectorParser().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			projector, err := parser.Parse(include, exclude)
			Expect(err).ToNot(HaveOccurred())

			// Evaluate the projector:
			path, err := NewPathEvaluator().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			evaluator, err := NewProjectorEvaluator().
				SetLogger(logger).
				SetPathEvaluator(path.Evaluate).
				Build()
			Expect(err).ToNot(HaveOccurred())
			actual, err := evaluator.Evaluate(context.Background(), projector, producer())
			Expect(err).ToNot(HaveOccurred())

			// Check the results:
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Include everything by default",
			"",
			"",
			func() any {
				type MyObject struct {
					MyField   string
					YourField string
				}
				return MyObject{
					MyField:   "myvalue",
					YourField: "yourvalue",
				}
			},
			map[string]any{
				"MyField":   "myvalue",
				"YourField": "yourvalue",
			},
		),
		Entry(
			"Include top level string",
			"MyField",
			"",
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
			"Include nested string",
			"MyField/YourField",
			"",
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
		Entry(
			"Include multiple subfields",
			"extensions/country,extensions/version,extensions/hub",
			"",
			func() any {
				return map[string]any{
					"extensions": map[string]any{
						"cert":    "my-cert",
						"key":     "my-key",
						"country": "ES",
						"version": "1.2.3",
						"hub":     "hub0",
					},
				}
			},
			map[string]any{
				"extensions": map[string]any{
					"country": "ES",
					"version": "1.2.3",
					"hub":     "hub0",
				},
			},
		),
		Entry(
			"Exclude top level string",
			"",
			"YourField",
			func() any {
				type MyObject struct {
					MyField   string
					YourField string
				}
				return MyObject{
					MyField:   "myvalue",
					YourField: "yourvalue",
				}
			},
			map[string]any{
				"MyField": "myvalue",
			},
		),
		Entry(
			"Exclude nested string",
			"",
			"MyField/YourField",
			func() any {
				type YourObject struct {
					YourField  string
					TheirField string
				}
				type MyObject struct {
					MyField YourObject
				}
				return MyObject{
					MyField: YourObject{
						YourField:  "yourvalue",
						TheirField: "theirvalue",
					},
				}
			},
			map[string]any{
				"MyField": map[string]any{
					"TheirField": "theirvalue",
				},
			},
		),
		Entry(
			"Include and then exclude",
			"MyField",
			"MyField",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			map[string]any{},
		),
		Entry(
			"Exclude non existing field",
			"MyField",
			"YourField",
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
	)
})

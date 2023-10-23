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
		func(src string, producer func() any, expected bool) {
			// Parse the expression:
			parser, err := NewParser().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			expr, err := parser.Parse(src)
			Expect(err).ToNot(HaveOccurred())

			// Evaluate the expression:
			resolver, err := NewResolver().
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
			"Eq string is true",
			"(eq,MyField,myvalue)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			true,
		),
		Entry(
			"Eq string is false",
			"(eq,MyField,yourvalue)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			false,
		),
		Entry(
			"Eq int is true",
			"(eq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Eq int is false",
			"(eq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 456,
				}
			},
			false,
		),
		Entry(
			"Cont is true",
			"(cont,MyField,my)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			true,
		),
		Entry(
			"Cont is false",
			"(cont,MyField,my)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "youvalue",
				}
			},
			false,
		),
		Entry(
			"Gt string is true",
			"(gt,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b",
				}
			},
			true,
		),
		Entry(
			"Gt string is false",
			"(gt,MyField,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			false,
		),
		Entry(
			"Gt same string is false",
			"(gt,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			false,
		),
		Entry(
			"Gt same int is false",
			"(gt,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			false,
		),
		Entry(
			"Gte int is true",
			"(gte,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 456,
				}
			},
			true,
		),
		Entry(
			"Gte int is false",
			"(gte,MyField,456)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			false,
		),
		Entry(
			"Gte same string is true",
			"(gte,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			true,
		),
		Entry(
			"Gte same int is true",
			"(gte,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Lt string is true",
			"(lt,MyField,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			true,
		),
		Entry(
			"Lt string is false",
			"(lt,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b",
				}
			},
			false,
		),
		Entry(
			"Lt same string is false",
			"(lt,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			false,
		),
		Entry(
			"Lt int is true",
			"(lt,MyField,456)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Lt int is false",
			"(lt,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 456,
				}
			},
			false,
		),
		Entry(
			"Lt same int is false",
			"(lt,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			false,
		),
		Entry(
			"Lte string is true",
			"(lte,MyField,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			true,
		),
		Entry(
			"Lte string is false",
			"(lte,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b",
				}
			},
			false,
		),
		Entry(
			"Lte same string is true",
			"(lte,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			true,
		),
		Entry(
			"Lte int is true",
			"(lte,MyField,456)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Lte int is false",
			"(lte,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 456,
				}
			},
			false,
		),
		Entry(
			"Lte same int is true",
			"(lte,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"In one string is true",
			"(in,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			true,
		),
		Entry(
			"In one string is false",
			"(in,MyField,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "a",
				}
			},
			false,
		),
		Entry(
			"In two strings is true",
			"(in,MyField,a,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b",
				}
			},
			true,
		),
		Entry(
			"In two strings is false",
			"(in,MyField,a,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "c",
				}
			},
			false,
		),
		Entry(
			"In one int is true",
			"(in,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"In one int is false",
			"(in,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Ncont one string is true",
			"(ncont,MyField,my)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "yourvalue",
				}
			},
			true,
		),
		Entry(
			"Ncont one string is false",
			"(ncont,MyField,my)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			false,
		),
		Entry(
			"Ncont two strings is true",
			"(ncont,MyField,my,your)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "theirvalue",
				}
			},
			true,
		),
		Entry(
			"Ncont two strings is false",
			"(ncont,MyField,my,your)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "yourvalue",
				}
			},
			false,
		),
		Entry(
			"Neq string is true",
			"(neq,MyField,myvalue)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "yourvalue",
				}
			},
			true,
		),
		Entry(
			"Neq string is false",
			"(neq,MyField,myvalue)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "myvalue",
				}
			},
			false,
		),
		Entry(
			"Neq int is true",
			"(neq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 456,
				}
			},
			true,
		),
		Entry(
			"Neq int is false",
			"(neq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			false,
		),
		Entry(
			"Nin one string is true",
			"(nin,MyField,a)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b",
				}
			},
			true,
		),
		Entry(
			"Nin one string is false",
			"(nin,MyField,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b",
				}
			},
			false,
		),
		Entry(
			"Nin two strings is true",
			"(nin,MyField,a,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "c",
				}
			},
			true,
		),
		Entry(
			"Nin two strings is false",
			"(nin,MyField,a,b)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b",
				}
			},
			false,
		),
		Entry(
			"Nin one int is true",
			"(nin,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 456,
				}
			},
			true,
		),
		Entry(
			"Nin one int is false",
			"(nin,MyField,123)",
			func() any {
				type MyObject struct {
					MyField int
				}
				return MyObject{
					MyField: 123,
				}
			},
			false,
		),
	)
})

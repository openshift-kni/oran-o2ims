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

var _ = Describe("Selector evaluator", func() {
	// nop is a simple path evaluator that always return nil.
	var nop = func(context.Context, Path, any) (any, error) {
		return nil, nil // nolint: nilnil
	}

	Describe("Creation", func() {
		It("Can be created with a logger and a path evaluator", func() {
			evaluator, err := NewSelectorEvaluator().
				SetLogger(logger).
				SetPathEvaluator(nop).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(evaluator).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			evaluator, err := NewSelectorEvaluator().
				SetPathEvaluator(nop).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(evaluator).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a path evaluator", func() {
			evaluator, err := NewSelectorEvaluator().
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
		func(src string, producer func() any, expected bool) {
			// Parse the selector:
			parser, err := NewSelectorParser().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			selector, err := parser.Parse(src)
			Expect(err).ToNot(HaveOccurred())

			// Evaluate the selector:
			path, err := NewPathEvaluator().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			evaluator, err := NewSelectorEvaluator().
				SetLogger(logger).
				SetPathEvaluator(path.Evaluate).
				Build()
			Expect(err).ToNot(HaveOccurred())
			actual, err := evaluator.Evaluate(context.Background(), selector, producer())
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
			"Eq string case insensitive for UUID values",
			"(eq,MyField,B8F14332-0331-45D9-8C0D-D24C25CC1F2E)",
			func() any {
				type MyObject struct {
					MyField string
				}
				return MyObject{
					MyField: "b8f14332-0331-45d9-8c0d-d24c25cc1f2e",
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
			"Eq float64 is true",
			"(eq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Eq float64 is false",
			"(eq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 456,
				}
			},
			false,
		),
		Entry(
			"Eq bool is true",
			"(eq,MyField,true)",
			func() any {
				type MyObject struct {
					MyField bool
				}
				return MyObject{
					MyField: true,
				}
			},
			true,
		),
		Entry(
			"Eq bool is false",
			"(eq,MyField,false)",
			func() any {
				type MyObject struct {
					MyField bool
				}
				return MyObject{
					MyField: true,
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
			"Gte float64 is false",
			"(gte,MyField,456)",
			func() any {
				type MyObject struct {
					MyField float64
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
			"Gte same float64 is true",
			"(gte,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
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
			"Lt float64 is true",
			"(lt,MyField,456)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Lt float64 is false",
			"(lt,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 456,
				}
			},
			false,
		),
		Entry(
			"Lt same float64 is false",
			"(lt,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
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
			"Lte float64 is true",
			"(lte,MyField,456)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 123,
				}
			},
			true,
		),
		Entry(
			"Lte float64 is false",
			"(lte,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 456,
				}
			},
			false,
		),
		Entry(
			"Lte same float64 is true",
			"(lte,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
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
			"Neq float64 is true",
			"(neq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 456,
				}
			},
			true,
		),
		Entry(
			"Neq float64 is false",
			"(neq,MyField,123)",
			func() any {
				type MyObject struct {
					MyField float64
				}
				return MyObject{
					MyField: 123,
				}
			},
			false,
		),
		Entry(
			"Neq bool is true",
			"(neq,MyField,false)",
			func() any {
				type MyObject struct {
					MyField bool
				}
				return MyObject{
					MyField: true,
				}
			},
			true,
		),
		Entry(
			"Neq bool is false",
			"(neq,MyField,true)",
			func() any {
				type MyObject struct {
					MyField bool
				}
				return MyObject{
					MyField: true,
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

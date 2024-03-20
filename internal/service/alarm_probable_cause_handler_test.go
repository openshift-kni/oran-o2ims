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

package service

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

var _ = Describe("Alarm probable cause handler", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			handler, err := NewAlarmProbableCauseHandler().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	Describe("Behaviour", func() {
		var (
			ctx     context.Context
			handler *AlarmProbableCauseHandler
		)

		BeforeEach(func() {
			var err error

			// Create a context:
			ctx = context.Background()

			// Create the handler:
			handler, err = NewAlarmProbableCauseHandler().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(handler).ToNot(BeNil())
		})

		AfterEach(func() {
		})

		Describe("List", func() {
			It("Translates non empty list of results", func() {
				// Send the request:
				response, err := handler.List(ctx, &ListRequest{})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				items, err := data.Collect(ctx, response.Items)
				Expect(err).ToNot(HaveOccurred())

				// Verify results
				for _, item := range items {
					Expect(item).To(HaveKey("probableCauseId"))
					Expect(item).To(HaveKey("name"))
					Expect(item).To(HaveKey("description"))
				}
			})

			It("Accepts a filter", func() {
				// Send the request:
				response, err := handler.List(ctx, &ListRequest{
					Selector: &search.Selector{
						Terms: []*search.Term{{
							Operator: search.Eq,
							Path: []string{
								"probableCauseId",
							},
							Values: []any{
								"Watchdog",
							},
						}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
			})
		})
	})
})

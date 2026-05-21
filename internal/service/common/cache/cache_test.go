/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCache(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Cache Suite")
}

var _ = ginkgo.Describe("Entry", func() {
	var ctx context.Context

	ginkgo.BeforeEach(func() {
		ctx = context.Background()
	})

	ginkgo.It("loads data on first Get", func() {
		callCount := 0
		entry := NewEntry("test", 0, func(_ context.Context) (string, error) {
			callCount++
			return "hello", nil
		})

		result, err := entry.Get(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("hello"))
		Expect(callCount).To(Equal(1))
	})

	ginkgo.It("returns cached data on subsequent Gets", func() {
		callCount := 0
		entry := NewEntry("test", 0, func(_ context.Context) (string, error) {
			callCount++
			return "hello", nil
		})

		_, _ = entry.Get(ctx)
		_, _ = entry.Get(ctx)
		result, err := entry.Get(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("hello"))
		Expect(callCount).To(Equal(1))
	})

	ginkgo.It("reloads after Invalidate", func() {
		callCount := 0
		entry := NewEntry("test", 0, func(_ context.Context) (int, error) {
			callCount++
			return callCount, nil
		})

		r1, _ := entry.Get(ctx)
		Expect(r1).To(Equal(1))

		entry.Invalidate()

		r2, _ := entry.Get(ctx)
		Expect(r2).To(Equal(2))
		Expect(callCount).To(Equal(2))
	})

	ginkgo.It("reloads after TTL expires", func() {
		callCount := 0
		entry := NewEntry("test", 50*time.Millisecond, func(_ context.Context) (int, error) {
			callCount++
			return callCount, nil
		})

		r1, _ := entry.Get(ctx)
		Expect(r1).To(Equal(1))

		time.Sleep(60 * time.Millisecond)

		r2, _ := entry.Get(ctx)
		Expect(r2).To(Equal(2))
	})

	ginkgo.It("does not reload before TTL expires", func() {
		callCount := 0
		entry := NewEntry("test", time.Hour, func(_ context.Context) (int, error) {
			callCount++
			return callCount, nil
		})

		_, _ = entry.Get(ctx)
		_, _ = entry.Get(ctx)
		Expect(callCount).To(Equal(1))
	})

	ginkgo.It("propagates loader errors", func() {
		entry := NewEntry("test", 0, func(_ context.Context) (string, error) {
			return "", fmt.Errorf("db error")
		})

		_, err := entry.Get(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("db error"))
	})

	ginkgo.It("retries after a failed load", func() {
		callCount := 0
		entry := NewEntry("test", 0, func(_ context.Context) (string, error) {
			callCount++
			if callCount == 1 {
				return "", fmt.Errorf("transient error")
			}
			return "recovered", nil
		})

		_, err := entry.Get(ctx)
		Expect(err).To(HaveOccurred())

		result, err := entry.Get(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("recovered"))
	})

	ginkgo.It("works with struct types", func() {
		type data struct {
			Items []string
			Count int
		}
		entry := NewEntry("test", 0, func(_ context.Context) (data, error) {
			return data{Items: []string{"a", "b"}, Count: 2}, nil
		})

		result, err := entry.Get(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Count).To(Equal(2))
		Expect(result.Items).To(Equal([]string{"a", "b"}))
	})
})

// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("sendNotification", func() {
	var (
		ctx    context.Context
		server *httptest.Server
	)

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	It("sends a JSON POST and succeeds on 204", func() {
		var receivedBody map[string]interface{}
		var receivedContentType string
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedContentType = r.Header.Get("Content-Type")
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &receivedBody)
			w.WriteHeader(http.StatusNoContent)
		}))

		event := Notification{
			NotificationID: uuid.New(),
			SequenceID:     1,
			Payload:        map[string]string{"key": "value"},
		}
		ctx = context.Background()
		err := sendNotification(ctx, server.Client(), server.URL, event)
		Expect(err).ToNot(HaveOccurred())
		Expect(receivedContentType).To(Equal("application/json"))
		Expect(receivedBody).To(HaveKeyWithValue("key", "value"))
	})

	It("succeeds on 200", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		ctx = context.Background()
		err := sendNotification(ctx, server.Client(), server.URL, Notification{Payload: "test"})
		Expect(err).ToNot(HaveOccurred())
	})

	It("fails on status > 204", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

		ctx = context.Background()
		err := sendNotification(ctx, server.Client(), server.URL, Notification{Payload: "test"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("notification failed"))
	})

	It("fails on connection error", func() {
		ctx = context.Background()
		err := sendNotification(ctx, http.DefaultClient, "http://127.0.0.1:1/bad", Notification{Payload: "test"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to send notification"))
	})
})

var _ = Describe("isRetryableError", func() {
	It("returns nil for retryable errors", func() {
		err := fmt.Errorf("some transient error")
		Expect(isRetryableError(err)).To(BeNil())
	})

	It("returns non-nil for DNS not found", func() {
		dnsErr := &net.DNSError{
			Err:        "no such host",
			Name:       "nonexistent.example.com",
			IsNotFound: true,
		}
		Expect(isRetryableError(dnsErr)).ToNot(BeNil())
		Expect(isRetryableError(dnsErr).Error()).To(ContainSubstring("host not found"))
	})

	It("returns non-nil for connection refused", func() {
		err := fmt.Errorf("dial: %w", syscall.ECONNREFUSED)
		Expect(isRetryableError(err)).ToNot(BeNil())
		Expect(isRetryableError(err).Error()).To(ContainSubstring("connection refused"))
	})

	It("treats DNS temporary errors as retryable", func() {
		dnsErr := &net.DNSError{
			Err:         "temporary failure",
			Name:        "example.com",
			IsNotFound:  false,
			IsTemporary: true,
		}
		Expect(isRetryableError(dnsErr)).To(BeNil())
	})
})

var _ = Describe("processEvent", func() {
	It("sends notification and reports completion", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		ctx := context.Background()
		completionCh := make(chan *SubscriptionJobComplete, 1)
		subID := uuid.New()
		notifID := uuid.New()

		event := Notification{
			NotificationID: notifID,
			SequenceID:     42,
			Payload:        "test",
		}

		processEvent(ctx, slog.Default(), server.Client(), completionCh, event, subID, server.URL)

		var result *SubscriptionJobComplete
		Eventually(completionCh).Should(Receive(&result))
		Expect(result.subscriptionID).To(Equal(subID))
		Expect(result.notificationID).To(Equal(notifID))
		Expect(result.sequenceID).To(Equal(42))
	})
})

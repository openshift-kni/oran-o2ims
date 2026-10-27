/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

var _ = Describe("NotificationTransformer", func() {
	var t *NotificationTransformer

	BeforeEach(func() {
		t = NewNotificationTransformer()
	})

	It("returns the original notification when consumer subscription id is nil", func() {
		sub := &notifier.SubscriptionInfo{ConsumerSubscriptionID: nil}
		n := &notifier.Notification{Payload: generated.ClusterChangeNotification{}}
		out, err := t.Transform(sub, n)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(BeIdenticalTo(n))
	})

	It("returns error when payload is not ClusterChangeNotification", func() {
		id := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		sub := &notifier.SubscriptionInfo{ConsumerSubscriptionID: &id}
		n := &notifier.Notification{Payload: "wrong"}
		_, err := t.Transform(sub, n)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not of type ClusterChangeNotification"))
	})

	It("clones payload and parses a UUID consumer subscription id back to a UUID", func() {
		consIDStr := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		consID := uuid.MustParse(consIDStr)
		sub := &notifier.SubscriptionInfo{SubscriptionID: uuid.New(), ConsumerSubscriptionID: &consIDStr}
		nid := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
		payload := generated.ClusterChangeNotification{
			NotificationEventType: 0,
			NotificationId:        nid,
		}
		n := &notifier.Notification{Payload: payload}

		out, err := t.Transform(sub, n)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).ToNot(BeIdenticalTo(n))
		typed, ok := out.Payload.(generated.ClusterChangeNotification)
		Expect(ok).To(BeTrue())
		Expect(typed.ConsumerSubscriptionId).ToNot(BeNil())
		Expect(*typed.ConsumerSubscriptionId).To(Equal(consID))
		Expect(typed.NotificationId).To(Equal(nid))
	})

	It("delivers the notification without consumer subscription id when it is not a valid UUID", func() {
		// consumerSubscriptionId is optional; a non-UUID value must not drop the
		// notification. The notification is still delivered, but with a nil id.
		//
		// TODO: Transform emits a WARN when consumerSubscriptionId fails to parse as a
		// UUID. Asserting that log line is deferred until a shared slog-capture test
		// helper exists; swapping slog.Default() here would race with parallel specs.
		invalid := "not-a-uuid"
		sub := &notifier.SubscriptionInfo{SubscriptionID: uuid.New(), ConsumerSubscriptionID: &invalid}
		nid := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
		payload := generated.ClusterChangeNotification{
			NotificationEventType: 0,
			NotificationId:        nid,
		}
		n := &notifier.Notification{Payload: payload}

		out, err := t.Transform(sub, n)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).ToNot(BeIdenticalTo(n))
		typed, ok := out.Payload.(generated.ClusterChangeNotification)
		Expect(ok).To(BeTrue())
		Expect(typed.ConsumerSubscriptionId).To(BeNil())
		Expect(typed.NotificationId).To(Equal(nid))
	})
})

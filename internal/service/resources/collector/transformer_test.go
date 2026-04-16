/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/api/generated"
)

var _ = Describe("NotificationTransformer", func() {
	var t *NotificationTransformer

	BeforeEach(func() {
		t = NewNotificationTransformer()
	})

	It("returns the original notification when consumer subscription id is nil", func() {
		sub := &notifier.SubscriptionInfo{ConsumerSubscriptionID: nil}
		n := &notifier.Notification{Payload: generated.InventoryChangeNotification{}}
		out, err := t.Transform(sub, n)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(BeIdenticalTo(n))
	})

	It("returns error when payload is not InventoryChangeNotification", func() {
		id := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
		sub := &notifier.SubscriptionInfo{ConsumerSubscriptionID: &id}
		n := &notifier.Notification{Payload: "wrong"}
		_, err := t.Transform(sub, n)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not of type InventoryChangeNotification"))
	})

	It("clones payload and sets consumer subscription id", func() {
		consID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
		sub := &notifier.SubscriptionInfo{ConsumerSubscriptionID: &consID}
		nid := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
		payload := generated.InventoryChangeNotification{
			NotificationEventType: 0,
			NotificationId:        nid,
		}
		n := &notifier.Notification{Payload: payload}

		out, err := t.Transform(sub, n)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeIdenticalTo(n))
		typed, ok := out.Payload.(generated.InventoryChangeNotification)
		Expect(ok).To(BeTrue())
		Expect(typed.ConsumerSubscriptionId).NotTo(BeNil())
		Expect(*typed.ConsumerSubscriptionId).To(Equal(consID))
		Expect(typed.NotificationId).To(Equal(nid))
	})
})

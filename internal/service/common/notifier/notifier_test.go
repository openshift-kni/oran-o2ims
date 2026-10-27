// SPDX-FileCopyrightText: Red Hat
//
// SPDX-License-Identifier: Apache-2.0
package notifier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

type stubClientProvider struct {
	client *http.Client
}

func (s *stubClientProvider) NewClient(_ context.Context, _ commonapi.AuthType) (*http.Client, error) {
	return s.client, nil
}

type stubSubscriptionProvider struct {
	subscriptions []SubscriptionInfo
	matchAll      bool
	updated       []*SubscriptionInfo
	// getFunc, when set, backs GetSubscription (by id). When nil, GetSubscription
	// returns svcutils.ErrNotFound.
	getFunc func(id uuid.UUID) (*SubscriptionInfo, error)
	mu      sync.Mutex
}

func (s *stubSubscriptionProvider) GetSubscriptions(_ context.Context) ([]SubscriptionInfo, error) {
	return s.subscriptions, nil
}

func (s *stubSubscriptionProvider) GetSubscription(_ context.Context, id uuid.UUID) (*SubscriptionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getFunc != nil {
		return s.getFunc(id)
	}
	return nil, svcutils.ErrNotFound
}

func (s *stubSubscriptionProvider) Matches(_ *SubscriptionInfo, _ *Notification) bool {
	return s.matchAll
}

func (s *stubSubscriptionProvider) UpdateSubscription(_ context.Context, sub *SubscriptionInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, sub)
	return nil
}

func (s *stubSubscriptionProvider) Transform(_ *SubscriptionInfo, notification *Notification) (*Notification, error) {
	return notification, nil
}

type stubNotificationProvider struct {
	notifications []Notification
	deleted       []uuid.UUID
	mu            sync.Mutex
}

func (s *stubNotificationProvider) GetNotifications(_ context.Context) ([]Notification, error) {
	return s.notifications, nil
}

func (s *stubNotificationProvider) DeleteNotification(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, id)
	return nil
}

var _ = Describe("NewNotifier", func() {
	It("creates a notifier with non-nil channels and worker map", func() {
		n := NewNotifier(&stubSubscriptionProvider{}, &stubNotificationProvider{}, &stubClientProvider{client: http.DefaultClient})
		Expect(n).ToNot(BeNil())
		Expect(n.workers).ToNot(BeNil())
		Expect(n.notificationChannel).ToNot(BeNil())
		Expect(n.subscriptionChannel).ToNot(BeNil())
	})
})

var _ = Describe("GetClientFactory", func() {
	It("returns the client provider", func() {
		cp := &stubClientProvider{client: http.DefaultClient}
		n := NewNotifier(&stubSubscriptionProvider{}, &stubNotificationProvider{}, cp)
		Expect(n.GetClientFactory()).To(Equal(cp))
	})
})

var _ = Describe("handleSubscriptionEvent", func() {
	It("adds a worker for a new subscription", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: server.Client()},
		)

		subID := uuid.New()
		ctx := context.Background()
		err := n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Removed: false,
			Subscription: &SubscriptionInfo{
				SubscriptionID: subID,
				Callback:       server.URL + "/cb",
			},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(n.workers).To(HaveKey(subID))
		n.workers[subID].Shutdown()
	})

	It("removes a worker for a deleted subscription", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: server.Client()},
		)

		subID := uuid.New()
		ctx := context.Background()
		_ = n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Removed: false,
			Subscription: &SubscriptionInfo{
				SubscriptionID: subID,
				Callback:       server.URL + "/cb",
			},
		})
		Expect(n.workers).To(HaveKey(subID))

		err := n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Removed: true,
			Subscription: &SubscriptionInfo{
				SubscriptionID: subID,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(n.workers).ToNot(HaveKey(subID))
	})

	It("handles removing a non-existent subscription gracefully", func() {
		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: http.DefaultClient},
		)

		ctx := context.Background()
		err := n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Removed: true,
			Subscription: &SubscriptionInfo{
				SubscriptionID: uuid.New(),
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})

	It("updates an existing worker in place on an update event", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		stub := &stubSubscriptionProvider{}
		n := NewNotifier(stub, &stubNotificationProvider{}, &stubClientProvider{client: server.Client()})

		subID := uuid.New()
		ctx := context.Background()
		Expect(n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Subscription: &SubscriptionInfo{SubscriptionID: subID, Callback: server.URL + "/old"},
		})).ToNot(HaveOccurred())
		Expect(n.workers).To(HaveKey(subID))

		// The provider returns the latest persisted values on re-read.
		newFilter := "CHANGE"
		newConsumer := "consumer-xyz"
		stub.getFunc = func(id uuid.UUID) (*SubscriptionInfo, error) {
			return &SubscriptionInfo{
				SubscriptionID:         id,
				Callback:               server.URL + "/new",
				Filter:                 &newFilter,
				ConsumerSubscriptionID: &newConsumer,
			}, nil
		}

		err := n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Updated:      true,
			Subscription: &SubscriptionInfo{SubscriptionID: subID},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(n.workers).To(HaveKey(subID))

		worker := n.workers[subID]
		worker.workMutex.Lock()
		gotCallback := worker.subscription.Callback
		gotFilter := worker.subscription.Filter
		gotConsumer := worker.subscription.ConsumerSubscriptionID
		worker.workMutex.Unlock()
		Expect(gotCallback).To(Equal(server.URL + "/new"))
		Expect(gotFilter).ToNot(BeNil())
		Expect(*gotFilter).To(Equal(newFilter))
		Expect(gotConsumer).ToNot(BeNil())
		Expect(*gotConsumer).To(Equal(newConsumer))
		worker.Shutdown()
	})

	It("tears down the worker when the subscription was deleted before the update applies", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		stub := &stubSubscriptionProvider{}
		n := NewNotifier(stub, &stubNotificationProvider{}, &stubClientProvider{client: server.Client()})

		subID := uuid.New()
		ctx := context.Background()
		_ = n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Subscription: &SubscriptionInfo{SubscriptionID: subID, Callback: server.URL + "/cb"},
		})
		Expect(n.workers).To(HaveKey(subID))

		// GetSubscription reports the subscription is gone -> worker is shut down and removed.
		stub.getFunc = func(uuid.UUID) (*SubscriptionInfo, error) { return nil, svcutils.ErrNotFound }

		err := n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Updated:      true,
			Subscription: &SubscriptionInfo{SubscriptionID: subID},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(n.workers).ToNot(HaveKey(subID))
	})

	It("defers an update event that arrives before the worker exists", func() {
		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: http.DefaultClient},
		)

		subID := uuid.New()
		err := n.handleSubscriptionEvent(context.Background(), &SubscriptionEvent{
			Updated:      true,
			Subscription: &SubscriptionInfo{SubscriptionID: subID},
		})
		Expect(err).ToNot(HaveOccurred())
		// The update is recorded as pending so the later creation event can apply it.
		Expect(n.pendingUpdates).To(HaveKey(subID))
		Expect(n.workers).ToNot(HaveKey(subID))
	})

	It("applies an update that arrived before the creation event", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		stub := &stubSubscriptionProvider{}
		n := NewNotifier(stub, &stubNotificationProvider{}, &stubClientProvider{client: server.Client()})

		subID := uuid.New()
		ctx := context.Background()

		// The update event is processed before the creation event (a create/PATCH race).
		Expect(n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Updated:      true,
			Subscription: &SubscriptionInfo{SubscriptionID: subID},
		})).ToNot(HaveOccurred())
		Expect(n.workers).ToNot(HaveKey(subID))

		// The PATCH persisted new values before the creation event is handled.
		newCallback := server.URL + "/patched"
		newFilter := "CHANGE"
		stub.getFunc = func(id uuid.UUID) (*SubscriptionInfo, error) {
			return &SubscriptionInfo{SubscriptionID: id, Callback: newCallback, Filter: &newFilter}, nil
		}

		// The creation event carries the stale creation-time snapshot...
		Expect(n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Subscription: &SubscriptionInfo{SubscriptionID: subID, Callback: server.URL + "/original"},
		})).ToNot(HaveOccurred())
		Expect(n.workers).To(HaveKey(subID))
		// ...and the deferred update is consumed.
		Expect(n.pendingUpdates).ToNot(HaveKey(subID))

		// The worker reflects the patched values, not the stale creation-time snapshot.
		worker := n.workers[subID]
		worker.workMutex.Lock()
		gotCallback := worker.subscription.Callback
		gotFilter := worker.subscription.Filter
		worker.workMutex.Unlock()
		Expect(gotCallback).To(Equal(newCallback))
		Expect(gotFilter).ToNot(BeNil())
		Expect(*gotFilter).To(Equal(newFilter))
		worker.Shutdown()
	})

	It("retries a failed update re-read until the worker converges", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		stub := &stubSubscriptionProvider{}
		n := NewNotifier(stub, &stubNotificationProvider{}, &stubClientProvider{client: server.Client()})
		n.subscriptionRefreshRetryDelay = 20 * time.Millisecond

		subID := uuid.New()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create the worker synchronously so we can hold a stable reference without racing
		// the worker map once the notifier loop is running.
		Expect(n.handleSubscriptionEvent(ctx, &SubscriptionEvent{
			Subscription: &SubscriptionInfo{SubscriptionID: subID, Callback: server.URL + "/original"},
		})).ToNot(HaveOccurred())
		worker := n.workers[subID]

		// The first re-read fails (transient DB error); subsequent re-reads succeed.
		newCallback := server.URL + "/patched"
		calls := 0
		stub.getFunc = func(id uuid.UUID) (*SubscriptionInfo, error) {
			calls++
			if calls == 1 {
				return nil, fmt.Errorf("transient db error")
			}
			return &SubscriptionInfo{SubscriptionID: id, Callback: newCallback}, nil
		}

		done := make(chan struct{})
		go func() {
			_ = n.Run(ctx)
			close(done)
		}()

		n.SubscriptionEvent(ctx, &SubscriptionEvent{
			Updated:      true,
			Subscription: &SubscriptionInfo{SubscriptionID: subID},
		})

		// The first attempt fails and is retried; the worker eventually converges on the
		// persisted callback once the re-read succeeds.
		Eventually(func() string {
			worker.workMutex.Lock()
			defer worker.workMutex.Unlock()
			return worker.subscription.Callback
		}, 5*time.Second, 20*time.Millisecond).Should(Equal(newCallback))

		cancel()
		Eventually(done, 2*time.Second).Should(BeClosed())
	})
})

var _ = Describe("handleNotification", func() {
	It("deletes notification when no subscriptions match", func() {
		notifProvider := &stubNotificationProvider{}
		n := NewNotifier(
			&stubSubscriptionProvider{matchAll: false},
			notifProvider,
			&stubClientProvider{client: http.DefaultClient},
		)

		ctx := context.Background()
		notifID := uuid.New()
		err := n.handleNotification(ctx, &Notification{
			NotificationID: notifID,
			SequenceID:     1,
			Payload:        "test",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(notifProvider.deleted).To(ContainElement(notifID))
	})
})

var _ = Describe("NewSubscriptionWorker", func() {
	It("creates a worker with the correct subscription", func() {
		ctx := context.Background()
		completionCh := make(chan *SubscriptionJobComplete, 1)
		sub := &SubscriptionInfo{
			SubscriptionID: uuid.New(),
			Callback:       "https://svc.ns.svc.cluster.local/cb",
		}

		worker, err := NewSubscriptionWorker(ctx, &stubClientProvider{client: http.DefaultClient}, completionCh, sub)
		Expect(err).ToNot(HaveOccurred())
		Expect(worker).ToNot(BeNil())
		Expect(worker.subscription).To(Equal(sub))
		worker.Shutdown()
	})
})

var _ = Describe("SubscriptionWorker", func() {
	It("processes notifications and reports completion", func() {
		var mu sync.Mutex
		received := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			received++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		completionCh := make(chan *SubscriptionJobComplete, 10)
		sub := &SubscriptionInfo{
			SubscriptionID: uuid.New(),
			Callback:       server.URL + "/cb",
		}

		worker, err := NewSubscriptionWorker(ctx, &stubClientProvider{client: server.Client()}, completionCh, sub)
		Expect(err).ToNot(HaveOccurred())

		go worker.Run()

		notifID := uuid.New()
		worker.NewNotification(&Notification{
			NotificationID: notifID,
			SequenceID:     1,
			Payload:        map[string]string{"test": "data"},
		})

		var result *SubscriptionJobComplete
		Eventually(completionCh, 5*time.Second).Should(Receive(&result))
		Expect(result.notificationID).To(Equal(notifID))
		Expect(result.sequenceID).To(Equal(1))

		mu.Lock()
		Expect(received).To(Equal(1))
		mu.Unlock()

		worker.Shutdown()
	})

	It("queues multiple notifications", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		completionCh := make(chan *SubscriptionJobComplete, 10)
		sub := &SubscriptionInfo{
			SubscriptionID: uuid.New(),
			Callback:       server.URL + "/cb",
		}

		worker, err := NewSubscriptionWorker(ctx, &stubClientProvider{client: server.Client()}, completionCh, sub)
		Expect(err).ToNot(HaveOccurred())

		go worker.Run()

		for i := 0; i < 3; i++ {
			worker.NewNotification(&Notification{
				NotificationID: uuid.New(),
				SequenceID:     i + 1,
				Payload:        fmt.Sprintf("event-%d", i),
			})
		}

		for i := 0; i < 3; i++ {
			Eventually(completionCh, 5*time.Second).Should(Receive())
		}

		worker.Shutdown()
	})
})

var _ = Describe("Notifier integration", func() {
	It("runs, loads subscriptions, dispatches and completes notifications", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		subID := uuid.New()
		notifID := uuid.New()

		subProvider := &stubSubscriptionProvider{
			matchAll: true,
			subscriptions: []SubscriptionInfo{
				{
					SubscriptionID: subID,
					Callback:       server.URL + "/cb",
					EventCursor:    0,
				},
			},
		}
		notifProvider := &stubNotificationProvider{
			notifications: []Notification{
				{
					NotificationID: notifID,
					SequenceID:     1,
					Payload:        "initial-event",
				},
			},
		}

		n := NewNotifier(subProvider, notifProvider, &stubClientProvider{client: server.Client()})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})

		go func() {
			_ = n.Run(ctx)
			close(done)
		}()

		Eventually(func() []uuid.UUID {
			notifProvider.mu.Lock()
			defer notifProvider.mu.Unlock()
			return notifProvider.deleted
		}, 10*time.Second, 100*time.Millisecond).Should(ContainElement(notifID))

		cancel()
		Eventually(done, 2*time.Second).Should(BeClosed())
	})
})

var _ = Describe("Notify", func() {
	It("sends a notification to the channel", func() {
		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: http.DefaultClient},
		)

		ctx := context.Background()
		notif := &Notification{
			NotificationID: uuid.New(),
			SequenceID:     1,
			Payload:        "test",
		}
		go n.Notify(ctx, notif)
		var received *Notification
		Eventually(n.notificationChannel, 2*time.Second).Should(Receive(&received))
		Expect(received.NotificationID).To(Equal(notif.NotificationID))
	})

	It("aborts when context is canceled", func() {
		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: http.DefaultClient},
		)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		notif := &Notification{NotificationID: uuid.New(), SequenceID: 1, Payload: "test"}
		done := make(chan struct{})
		go func() {
			n.Notify(ctx, notif)
			close(done)
		}()
		Eventually(done, 2*time.Second).Should(BeClosed())
	})
})

var _ = Describe("SubscriptionEvent method", func() {
	It("sends a subscription event to the channel", func() {
		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: http.DefaultClient},
		)

		ctx := context.Background()
		event := &SubscriptionEvent{
			Removed:      false,
			Subscription: &SubscriptionInfo{SubscriptionID: uuid.New()},
		}
		go n.SubscriptionEvent(ctx, event)
		var received *SubscriptionEvent
		Eventually(n.subscriptionChannel, 2*time.Second).Should(Receive(&received))
		Expect(received.Subscription.SubscriptionID).To(Equal(event.Subscription.SubscriptionID))
	})

	It("aborts when context is canceled", func() {
		n := NewNotifier(
			&stubSubscriptionProvider{},
			&stubNotificationProvider{},
			&stubClientProvider{client: http.DefaultClient},
		)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := &SubscriptionEvent{
			Removed:      false,
			Subscription: &SubscriptionInfo{SubscriptionID: uuid.New()},
		}
		done := make(chan struct{})
		go func() {
			n.SubscriptionEvent(ctx, event)
			close(done)
		}()
		Eventually(done, 2*time.Second).Should(BeClosed())
	})
})

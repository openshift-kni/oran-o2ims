/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

// DefaultBufferedChannelSize defines the default size for buffered channels used across the notifier.
const DefaultBufferedChannelSize = 100

// DefaultSubscriptionRefreshRetryDelay is the default delay before the notifier retries applying a
// subscription update whose authoritative re-read failed with a transient (non-NotFound) error.
const DefaultSubscriptionRefreshRetryDelay = 5 * time.Second

// CompletionChannelSize defines the buffer size of the completion channel.  We keep this small to ensure that we don't
// process a large number of notifications without first updating the subscription cursor so workers will block until
// completions are processed before sending more notifications.
const CompletionChannelSize = 1

// Notification defines a generic notification object.  The payload should support JSON marshaling.
type Notification struct {
	NotificationID uuid.UUID
	SequenceID     int
	Payload        interface{}
}

// SubscriptionInfo defines a generic subscription object.  The intent is to abstract away the differences between the
// alarm and resource server subscription models.
type SubscriptionInfo struct {
	SubscriptionID uuid.UUID
	// ConsumerSubscriptionID is the consumer-provided subscription identifier.
	// It is a free-form string (not restricted to UUID) so that it can carry
	// non-UUID values from all subscription sources (M-10).
	ConsumerSubscriptionID *string
	Callback               string
	Filter                 *string
	EventCursor            int
}

// NotificationProvider must be implemented by a domain specific model implementor so that the notifier can manage
// notifications on its behalf.
type NotificationProvider interface {
	GetNotifications(ctx context.Context) ([]Notification, error)
	DeleteNotification(ctx context.Context, notificationID uuid.UUID) error
}

// SubscriptionProvider must be implemented by a domain specific model implementor so that the notifier can manage
// subscriptions on its behalf.
type SubscriptionProvider interface {
	GetSubscriptions(ctx context.Context) ([]SubscriptionInfo, error)
	// GetSubscription returns a single subscription by id. It must preserve
	// svcutils.ErrNotFound (wrapped with %w) so callers can detect deletion.
	GetSubscription(ctx context.Context, id uuid.UUID) (*SubscriptionInfo, error)
	Matches(subscription *SubscriptionInfo, notification *Notification) bool
	UpdateSubscription(ctx context.Context, subscription *SubscriptionInfo) error
	Transform(subscription *SubscriptionInfo, notification *Notification) (*Notification, error)
}

// SubscriptionEvent defines the information sent to the notifier when a subscription is added/removed/updated
type SubscriptionEvent struct {
	// Removed defines whether the subscription has been removed or added
	Removed bool
	// Updated defines whether an existing subscription has been modified in place (e.g. via PATCH).
	// When set, only Subscription.SubscriptionID is required; the notifier re-reads the latest
	// persisted values from the SubscriptionProvider.
	Updated bool
	// Subscription is the subscription being added/removed/updated
	Subscription *SubscriptionInfo
}

// SubscriptionEventHandler is an interfaces which defines the operations required to be supported by a component that
// is prepared to handle subscription events.
type SubscriptionEventHandler interface {
	SubscriptionEvent(ctx context.Context, event *SubscriptionEvent)
	GetClientFactory() ClientProvider
}

// Notifier represents the data required by the notification process
type Notifier struct {
	// oauthConfig defines the oauth related attributes used to establish connections to subscribers
	clientProvider ClientProvider
	// notificationChannel is used to receive notifications about new events from the collector
	notificationChannel chan *Notification
	// subscriptionChannel is used to receive notifications about new/deleted subscriptions
	subscriptionChannel chan *SubscriptionEvent
	// subscriptionJobCompleteChannel is used to be notified by a worker that it has completed
	// handling a notification.
	subscriptionJobCompleteChannel chan *SubscriptionJobComplete
	// workers is the list of workers started to service subscriptions.  It is mapped by
	// subscription uuid value.
	workers map[uuid.UUID]*SubscriptionWorker
	// pendingUpdates tracks subscriptions whose update event arrived before their creation
	// event (a create/PATCH race). The flag is consumed when the creation event is handled,
	// triggering an immediate re-read so the new worker reflects the latest persisted values
	// instead of the stale creation-time snapshot.
	pendingUpdates map[uuid.UUID]struct{}
	// subscriptionRefreshRetryDelay is the delay before retrying a subscription update whose
	// authoritative re-read failed with a transient (non-NotFound) error.
	subscriptionRefreshRetryDelay time.Duration
	// notificationProvider is a plugable interface which provides persistence handling for notifications
	notificationProvider NotificationProvider
	// subscriptionProvider is a plugable interface which provides persistence handling for subscriptions
	subscriptionProvider SubscriptionProvider
}

// NewNotifier creates a new instance of a Notifier
func NewNotifier(subscriptionProvider SubscriptionProvider, notificationProvider NotificationProvider,
	clientProvider ClientProvider) *Notifier {
	eventChannel := make(chan *Notification, DefaultBufferedChannelSize)
	subscriptionChannel := make(chan *SubscriptionEvent, DefaultBufferedChannelSize)
	subscriberJobCompleteChannel := make(chan *SubscriptionJobComplete, CompletionChannelSize)
	return &Notifier{
		clientProvider:                 clientProvider,
		subscriptionProvider:           subscriptionProvider,
		notificationProvider:           notificationProvider,
		notificationChannel:            eventChannel,
		subscriptionChannel:            subscriptionChannel,
		subscriptionJobCompleteChannel: subscriberJobCompleteChannel,
		workers:                        make(map[uuid.UUID]*SubscriptionWorker),
		pendingUpdates:                 make(map[uuid.UUID]struct{}),
		subscriptionRefreshRetryDelay:  DefaultSubscriptionRefreshRetryDelay,
	}
}

// Run executes the notifier main loop to respond to changes to the database contents
func (n *Notifier) Run(ctx context.Context) error {
	if err := n.init(ctx); err != nil {
		return err
	}

	for {
		select {
		case e := <-n.subscriptionJobCompleteChannel:
			if err := n.handleSubscriptionJobCompleteEvent(ctx, e); err != nil {
				slog.ErrorContext(ctx, "failed to handle subscription job complete event", slog.Any("error", err))
			}
		case e := <-n.notificationChannel:
			if err := n.handleNotification(ctx, e); err != nil {
				slog.ErrorContext(ctx, "failed to handle notification",
					slog.String("notificationID", e.NotificationID.String()), slog.Int("sequenceID", e.SequenceID), slog.Any("error", err))
			}
		case e := <-n.subscriptionChannel:
			if err := n.handleSubscriptionEvent(ctx, e); err != nil {
				slog.ErrorContext(ctx, "failed to handle subscription event", slog.Any("error", err))
			}
		case <-ctx.Done():
			n.shutdownWorkers()
			slog.InfoContext(ctx, "context terminated; notifier exiting")
			return nil
		}
	}
}

// shutdownWorkers shuts down each worker
func (n *Notifier) shutdownWorkers() {
	for _, worker := range n.workers {
		worker.Shutdown()
	}
}

// init runs the onetime initialization steps for the notifier
func (n *Notifier) init(ctx context.Context) error {
	slog.InfoContext(ctx, "initializing notifier")
	if err := n.loadSubscriptions(ctx); err != nil {
		return fmt.Errorf("failed to load subscriptions: %w", err)
	}
	if err := n.loadEvents(ctx); err != nil {
		return fmt.Errorf("failed to load events: %w", err)
	}
	return nil
}

// loadEvents loads the current set of notifications from the database
func (n *Notifier) loadEvents(ctx context.Context) error {
	slog.InfoContext(ctx, "loading events")

	notifications, err := n.notificationProvider.GetNotifications(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notifications: %w", err)
	}

	// Sort them by sequence id to ensure we process them in order
	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i].SequenceID < notifications[j].SequenceID
	})

	// Dispatch each event to the workers
	for _, notification := range notifications {
		if err := n.handleNotification(ctx, &notification); err != nil {
			return fmt.Errorf("failed to handle notification: %w", err)
		}
	}

	slog.InfoContext(ctx, "loaded events", slog.Int("count", len(notifications)))
	return nil
}

// loadSubscriptions loads the current set of subscriptions from the database
func (n *Notifier) loadSubscriptions(ctx context.Context) error {
	slog.InfoContext(ctx, "loading subscriptions")

	subscriptions, err := n.subscriptionProvider.GetSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	for _, s := range subscriptions {
		subscriptionID := s.SubscriptionID
		n.workers[subscriptionID], err = NewSubscriptionWorker(ctx, n.clientProvider, n.subscriptionJobCompleteChannel, &s)
		if err != nil {
			return fmt.Errorf("failed to create subscription worker: %w", err)
		}

		go n.workers[subscriptionID].Run()
	}

	slog.InfoContext(ctx, "subscriptions loaded", slog.Int("count", len(n.workers)))

	return nil
}

// Notify should be used to signal that a database change has occurred
func (n *Notifier) Notify(ctx context.Context, event *Notification) {
	select {
	case n.notificationChannel <- event:
	case <-ctx.Done():
		slog.InfoContext(ctx, "context terminated; aborting Notify attempt")
	}
}

// handleNotification handles an incoming notification
func (n *Notifier) handleNotification(ctx context.Context, event *Notification) error {
	slog.InfoContext(ctx, "handling notification", slog.String("notificationID", event.NotificationID.String()), slog.Int("sequenceID", event.SequenceID))

	count := 0
	for _, worker := range n.workers {
		if n.subscriptionProvider.Matches(worker.subscription, event) {
			clone, err := n.subscriptionProvider.Transform(worker.subscription, event)
			if err != nil {
				slog.ErrorContext(ctx, "failed to transform notification", slog.String("subscription", worker.subscription.SubscriptionID.String()), slog.Any("error", err))
				continue
			}
			worker.NewNotification(clone)
			count++
		}
	}

	if count == 0 {
		// No subscriptions matched just delete the event
		slog.DebugContext(ctx, "no matching subscriptions; deleting event",
			slog.String("notificationID", event.NotificationID.String()), slog.Int("sequenceID", event.SequenceID))
		if err := n.notificationProvider.DeleteNotification(ctx, event.NotificationID); err != nil {
			return fmt.Errorf("failed to delete notification: %w", err)
		}
		return nil
	}

	slog.InfoContext(ctx, "notification dispatched",
		slog.String("notificationID", event.NotificationID.String()), slog.Int("sequenceID", event.SequenceID),
		slog.Int("subscribers", count))

	return nil
}

// SubscriptionEvent should be used to signal that a change to a subscription has occurred
func (n *Notifier) SubscriptionEvent(ctx context.Context, event *SubscriptionEvent) {
	select {
	case n.subscriptionChannel <- event:
	case <-ctx.Done():
		slog.InfoContext(ctx, "context terminated; aborting SubscriptionEvent attempt")
	}
}

// GetClientFactory return the underlying factory to create httpclient that can be used reach callback url
func (n *Notifier) GetClientFactory() ClientProvider {
	return n.clientProvider
}

// releaseNotification deletes a notification if it does not match any active subscriptions.
func (n *Notifier) releaseNotification(ctx context.Context, notificationID uuid.UUID, sequenceID int) error {
	done := true
	for _, worker := range n.workers {
		if worker.subscription.EventCursor < sequenceID {
			done = false
			break
		}
	}

	if done {
		if err := n.notificationProvider.DeleteNotification(ctx, notificationID); err != nil {
			return fmt.Errorf("failed to delete completed notification: %w", err)
		}
	}

	return nil
}

// releaseNotifications deletes all notifications that do not match any active subscriptions.
func (n *Notifier) releaseNotifications(ctx context.Context, events []*Notification) {
	for _, event := range events {
		err := n.releaseNotification(ctx, event.NotificationID, event.SequenceID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to release event", slog.Any("error", err))
		}
	}
}

// handleSubscriptionEvent handles an incoming subscription change event
func (n *Notifier) handleSubscriptionEvent(ctx context.Context, event *SubscriptionEvent) error {
	subscriptionID := event.Subscription.SubscriptionID
	slog.InfoContext(ctx, "Handling subscription event", slog.Bool("removed", event.Removed), slog.Bool("updated", event.Updated), slog.Any("subscription", event.Subscription))

	if event.Updated {
		// An existing subscription was modified in place (e.g. via PATCH).
		if _, found := n.workers[subscriptionID]; !found {
			// The creation event for this subscription has not been processed yet (a
			// create/PATCH race where the update event was queued ahead of creation).
			// Defer the update so the creation handler re-reads the latest persisted
			// values instead of installing the stale creation-time snapshot.
			slog.DebugContext(ctx, "subscription worker not found for update; deferring until creation", slog.String("subscriptionID", subscriptionID.String()))
			n.pendingUpdates[subscriptionID] = struct{}{}
			return nil
		}
		return n.refreshSubscriptionWorker(ctx, subscriptionID)
	}

	if event.Removed {
		// The subscription has been removed.  Cleanup any associated data.
		// Drop any update that was deferred waiting for creation; the subscription is gone.
		delete(n.pendingUpdates, subscriptionID)

		worker, found := n.workers[subscriptionID]
		if !found {
			slog.DebugContext(ctx, "subscription worker not found", slog.String("subscriptionID", subscriptionID.String()))
			return nil
		}

		// shutdown the worker.
		worker.Shutdown()
		delete(n.workers, event.Subscription.SubscriptionID)
		// attempt to release any notifications that were queued by this worker.
		n.releaseNotifications(ctx, worker.GetNotifications())
	} else {
		worker, err := NewSubscriptionWorker(ctx, n.clientProvider, n.subscriptionJobCompleteChannel, event.Subscription)
		if err != nil {
			return fmt.Errorf("failed to create subscription worker: %w", err)
		}

		n.workers[subscriptionID] = worker
		go worker.Run()

		if _, pending := n.pendingUpdates[subscriptionID]; pending {
			// An update event for this subscription arrived before this creation event.
			// Apply the latest persisted values now that the worker exists so it does not
			// run with the stale creation-time snapshot.
			delete(n.pendingUpdates, subscriptionID)
			if err := n.refreshSubscriptionWorker(ctx, subscriptionID); err != nil {
				return err
			}
		}
	}

	slog.InfoContext(ctx, "subscription event handled", slog.Int("workers", len(n.workers)))
	return nil
}

// refreshSubscriptionWorker re-reads the authoritative persisted subscription and applies the
// latest values to the live worker in place, preserving its cursor, queue, in-flight work, and
// client. If the subscription was deleted it tears the worker down; on a transient read error it
// re-queues the update so the worker eventually converges on the persisted values once the
// datastore recovers, rather than continuing to notify with stale settings.
func (n *Notifier) refreshSubscriptionWorker(ctx context.Context, subscriptionID uuid.UUID) error {
	worker, found := n.workers[subscriptionID]
	if !found {
		// The DB is authoritative; a missing worker means nothing to update.
		slog.DebugContext(ctx, "subscription worker not found for update; ignoring", slog.String("subscriptionID", subscriptionID.String()))
		return nil
	}

	latest, err := n.subscriptionProvider.GetSubscription(ctx, subscriptionID)
	if errors.Is(err, svcutils.ErrNotFound) {
		// The subscription was deleted between the PATCH and this event; tear the worker down.
		slog.DebugContext(ctx, "subscription removed before update applied; shutting down worker", slog.String("subscriptionID", subscriptionID.String()))
		worker.Shutdown()
		delete(n.workers, subscriptionID)
		n.releaseNotifications(ctx, worker.GetNotifications())
		return nil
	}
	if err != nil {
		// Transient failure re-reading the subscription; re-queue the update so the worker
		// eventually converges on the persisted values once the datastore recovers.
		slog.ErrorContext(ctx, "failed to re-read subscription for update; scheduling retry", slog.String("subscriptionID", subscriptionID.String()), slog.Any("error", err))
		n.scheduleSubscriptionUpdateRetry(ctx, subscriptionID)
		return nil
	}

	// The effective filter/callback/consumer id already come through the
	// provider's converter, so no recomputation is needed here.
	worker.UpdateSubscription(latest)
	slog.InfoContext(ctx, "subscription worker updated in place", slog.String("subscriptionID", subscriptionID.String()))
	return nil
}

// scheduleSubscriptionUpdateRetry re-queues an update event for the given subscription after a
// short delay so the notifier eventually converges on the persisted subscription once a transient
// datastore error clears. It is non-blocking and abandons the retry if the context is cancelled.
func (n *Notifier) scheduleSubscriptionUpdateRetry(ctx context.Context, subscriptionID uuid.UUID) {
	go func() {
		select {
		case <-time.After(n.subscriptionRefreshRetryDelay):
		case <-ctx.Done():
			return
		}
		n.SubscriptionEvent(ctx, &SubscriptionEvent{
			Updated:      true,
			Subscription: &SubscriptionInfo{SubscriptionID: subscriptionID},
		})
	}()
}

// handleSubscriptionJobCompleteEvent handles a job completion event, removes the subscriber from the event job,
func (n *Notifier) handleSubscriptionJobCompleteEvent(ctx context.Context, event *SubscriptionJobComplete) error {
	slog.DebugContext(ctx, "handling subscription job complete event",
		slog.String("notificationID", event.notificationID.String()), slog.String("subscriptionID", event.subscriptionID.String()))

	// Lookup the subscription worker for this event
	if worker, found := n.workers[event.subscriptionID]; found {
		// Update the subscription's event cursor.
		subscription := worker.subscription
		subscription.EventCursor = event.sequenceID
		if err := n.subscriptionProvider.UpdateSubscription(ctx, subscription); err != nil {
			return fmt.Errorf("failed to update subscription: %w", err)
		}
	} else {
		// Likely has been deleted and this is a race condition.
		slog.DebugContext(ctx, "subscription worker not found", slog.String("subscriptionID", event.subscriptionID.String()))
	}

	return n.releaseNotification(ctx, event.notificationID, event.sequenceID)
}

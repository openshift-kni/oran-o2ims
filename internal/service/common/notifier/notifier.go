package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"
)

// DefaultBufferedChannelSize defines the default size for buffered channels used across the notifier.
const DefaultBufferedChannelSize = 100

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
	SubscriptionID         uuid.UUID
	ConsumerSubscriptionID *uuid.UUID
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
	Matches(subscription *SubscriptionInfo, notification *Notification) bool
	UpdateSubscription(ctx context.Context, subscription *SubscriptionInfo) error
	Transform(subscription *SubscriptionInfo, notification *Notification) (*Notification, error)
}

// SubscriptionEvent defines the information sent to the notifier when a subscription is added/removed
type SubscriptionEvent struct {
	// Removed defines whether the subscription has been removed or added
	Removed bool
	// Subscription is the subscription being added/removed
	Subscription *SubscriptionInfo
}

// SubscriptionEventHandler is an interfaces which defines the operations required to be supported by a component that
// is prepared to handle subscription events.
type SubscriptionEventHandler interface {
	SubscriptionEvent(ctx context.Context, event *SubscriptionEvent)
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
				slog.Error("failed to handle subscription job complete event", "error", err)
			}
		case e := <-n.notificationChannel:
			if err := n.handleNotification(ctx, e); err != nil {
				slog.Error("failed to handle notification",
					"NotificationID", e.NotificationID, "sequenceID", e.SequenceID, "error", err)
			}
		case e := <-n.subscriptionChannel:
			if err := n.handleSubscriptionEvent(ctx, e); err != nil {
				slog.Error("failed to handle subscription event", "error", err)
			}
		case <-ctx.Done():
			n.shutdownWorkers()
			slog.Info("context terminated; notifier exiting")
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
	slog.Info("initializing notifier")
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
	slog.Info("loading events")

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

	slog.Info("loaded events", "count", len(notifications))
	return nil
}

// loadSubscriptions loads the current set of subscriptions from the database
func (n *Notifier) loadSubscriptions(ctx context.Context) error {
	slog.Info("loading subscriptions")

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

	slog.Info("subscriptions loaded", "count", len(n.workers))

	return nil
}

// Notify should be used to signal that a database change has occurred
func (n *Notifier) Notify(ctx context.Context, event *Notification) {
	select {
	case n.notificationChannel <- event:
	case <-ctx.Done():
		slog.Info("context terminated; aborting Notify attempt")
	}
}

// handleNotification handles an incoming notification
func (n *Notifier) handleNotification(ctx context.Context, event *Notification) error {
	slog.Info("handling notification", "NotificationID", event.NotificationID, "sequenceID", event.SequenceID)

	count := 0
	for _, worker := range n.workers {
		if n.subscriptionProvider.Matches(worker.subscription, event) {
			clone, err := n.subscriptionProvider.Transform(worker.subscription, event)
			if err != nil {
				slog.Error("failed to transform notification", "subscription", worker.subscription.SubscriptionID, "error", err)
				continue
			}
			worker.NewNotification(clone)
			count++
		}
	}

	if count == 0 {
		// No subscriptions matched just delete the event
		slog.Debug("no matching subscriptions; deleting event",
			"NotificationID", event.NotificationID, "sequenceID", event.SequenceID)
		if err := n.notificationProvider.DeleteNotification(ctx, event.NotificationID); err != nil {
			return fmt.Errorf("failed to delete notification: %w", err)
		}
		return nil
	}

	slog.Info("notification dispatched",
		"NotificationID", event.NotificationID, "sequenceID", event.SequenceID,
		"subscribers", count)

	return nil
}

// SubscriptionEvent should be used to signal that a change to a subscription has occurred
func (n *Notifier) SubscriptionEvent(ctx context.Context, event *SubscriptionEvent) {
	select {
	case n.subscriptionChannel <- event:
	case <-ctx.Done():
		slog.Info("context terminated; aborting SubscriptionEvent attempt")
	}
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
			slog.Error("failed to release event", "error", err)
		}
	}
}

// handleSubscriptionEvent handles an incoming subscription change event
func (n *Notifier) handleSubscriptionEvent(ctx context.Context, event *SubscriptionEvent) error {
	subscriptionID := event.Subscription.SubscriptionID
	slog.Info("Handling subscription event", "removed", event.Removed, "subscription", event.Subscription)

	if event.Removed {
		// The subscription has been removed.  Cleanup any associated data.
		worker, found := n.workers[subscriptionID]
		if !found {
			slog.Debug("subscription worker not found", "subscriptionID", subscriptionID)
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
	}

	slog.Info("subscription event handled", "workers", len(n.workers))
	return nil
}

// handleSubscriptionJobCompleteEvent handles a job completion event, removes the subscriber from the event job,
func (n *Notifier) handleSubscriptionJobCompleteEvent(ctx context.Context, event *SubscriptionJobComplete) error {
	slog.Debug("handling subscription job complete event",
		"NotificationID", event.notificationID, "subscriptionID", event.subscriptionID)

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
		slog.Debug("subscription worker not found", "subscriptionID", event.subscriptionID)
	}

	return n.releaseNotification(ctx, event.notificationID, event.sequenceID)
}

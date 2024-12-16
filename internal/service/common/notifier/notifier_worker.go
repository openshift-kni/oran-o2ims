package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// maxRetries defines the number of attempts made on each notification
const maxRetries = 5

// retryDelay defines the amount of time between each successive notification attempt
const retryDelay = 10 * time.Second // TODO: increase

// SubscriptionJob is the event sent to the subscription worker to have it process the data change event
type SubscriptionJob struct {
	notification *Notification
}

// SubscriptionJobComplete is the event sent from the subscription worker to the notifier to report that it has
// successfully sent a notification for the data change event
type SubscriptionJobComplete struct {
	subscriptionID uuid.UUID
	notificationID uuid.UUID
	sequenceID     int
}

// SubscriptionWorker is a placeholder which represents a go routine created to monitor events for a subscription
type SubscriptionWorker struct {
	// subscription is the subscription being monitored for events
	subscription *SubscriptionInfo
	// workChannel is the channel to be used to sent work to the worker
	workChannel chan *SubscriptionJob
	// subscriptionJobCompleteChannel is the channel to be used to report back to the notifier when an event is complete
	subscriptionJobCompleteChannel chan *SubscriptionJobComplete
	// ctx is the context passed to the go routine.  If the subscription is canceled or the server is stopping the
	// context will be canceled
	ctx context.Context
	// cancel is the CancelFunc associated to the worker context.
	cancel context.CancelFunc
	// events represents the list of work to be done by the worker
	events []*Notification
	// currentEvent is the event currently being processed
	currentEvent *Notification
	// currentEventDone signals back to the worker that the current event has been processed
	currentEventDone chan *SubscriptionJobComplete
	// client is used to communicate to the subscriber
	client *http.Client
	// logger is used to add info to each log produced by the worker
	logger *slog.Logger
}

// NewSubscriptionWorker creates a new subscription worker object to service a specific subscription
func NewSubscriptionWorker(ctx context.Context, subscriptionJobCompleteChannel chan *SubscriptionJobComplete,
	subscription *SubscriptionInfo) (*SubscriptionWorker, error) {
	// Create a client for this subscription.
	// TODO: fill in the oauth attributes from the SMO config passed to the server
	client, err := utils.SetupOAuthClient(ctx, utils.OAuthClientConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to setup oauth client: %w", err)
	}

	// Set up a custom logger to include the subscription info so it doesn't need to be repeated
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug, // TODO: set log level from server args
	}))
	logger = logger.With("subscription", subscription.SubscriptionID)

	workerCtx, cancel := context.WithCancel(ctx)
	return &SubscriptionWorker{
		subscription:                   subscription,
		workChannel:                    make(chan *SubscriptionJob, 1),
		subscriptionJobCompleteChannel: subscriptionJobCompleteChannel,
		cancel:                         cancel,
		ctx:                            workerCtx,
		currentEventDone:               make(chan *SubscriptionJobComplete, 1),
		client:                         client,
		logger:                         logger,
	}, nil
}

// NewNotification sends a data change event to a subscription worker
func (w *SubscriptionWorker) NewNotification(notification *Notification) {
	w.workChannel <- &SubscriptionJob{notification: notification}
}

// Shutdown terminates the worker and releases any pending events
func (w *SubscriptionWorker) Shutdown() {
	w.cancel()
}

// releaseEvents releases all pending events back to the notifier
func (w *SubscriptionWorker) releaseEvents() {
	for _, event := range w.events {
		w.subscriptionJobCompleteChannel <- &SubscriptionJobComplete{
			subscriptionID: w.subscription.SubscriptionID,
			notificationID: event.NotificationID,
			sequenceID:     event.SequenceID,
		}
	}
}

// Run executes that main loop for the worker handling events as they arrive.
func (w *SubscriptionWorker) Run() {
	w.logger.Info("subscription worker started", "callback", w.subscription.Callback)

	for {
		select {
		case e := <-w.currentEventDone:
			w.handleCurrentEventCompletion(e)
		case event := <-w.workChannel:
			w.handleSubscriptionJob(w.ctx, event)
		case <-w.ctx.Done():
			w.releaseEvents()
			w.logger.Info("subscription worker shutting down")
			return
		}
	}
}

// handleCurrentEventCompletion handles the end of processing the current event.
func (w *SubscriptionWorker) handleCurrentEventCompletion(e *SubscriptionJobComplete) {
	w.currentEvent = nil
	// forward to the notifier so that it can release the event
	w.subscriptionJobCompleteChannel <- e
	// handle the next event
	w.processNextEvent(w.ctx)
}

// handleSubscriptionJob receives a new subscription job and queues it for processing
func (w *SubscriptionWorker) handleSubscriptionJob(ctx context.Context, job *SubscriptionJob) {
	// Queue it
	w.events = append(w.events, job.notification)
	w.logger.Info("data change event job received",
		"notificationID", job.notification.NotificationID,
		"sequenceID", job.notification.SequenceID)

	if w.currentEvent == nil {
		w.currentEvent = job.notification
		go processEvent(ctx, w.logger, w.client, w.currentEventDone, *w.currentEvent, w.subscription.SubscriptionID, w.subscription.Callback)
	}
}

// processNextEvent looks for the next event to be processed.
func (w *SubscriptionWorker) processNextEvent(ctx context.Context) {
	if len(w.events) == 0 {
		// No more events
		w.logger.Debug("no more events to process")
		return
	}

	// Pop the first element off of the list and set it as the current event
	w.currentEvent, w.events = w.events[0], w.events[1:]

	// Launch a task to send the notification (or retry on failures)
	go processEvent(ctx, w.logger, w.client, w.currentEventDone, *w.currentEvent, w.subscription.SubscriptionID, w.subscription.Callback)
}

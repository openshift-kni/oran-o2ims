package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
)

// maxRetries defines the number of attempts made on each notification
const maxRetries = 5

// retryDelay defines the amount of time between each successive notification attempt
const retryDelay = 10 * time.Second // TODO: increase

// SubscriptionJobComplete is the event sent from the subscription worker to the notifier to report that it has
// successfully sent a notification for the data change event
type SubscriptionJobComplete struct {
	subscriptionID uuid.UUID
	notificationID uuid.UUID
	sequenceID     int
}

// SubscriptionWorker is a placeholder that represents a go routine created to monitor events for a subscription
type SubscriptionWorker struct {
	// subscription is the subscription being monitored for events
	subscription *SubscriptionInfo
	// workChannel is the channel used to send work to the worker
	workChannel chan struct{}
	// subscriptionJobCompleteChannel is the channel to be used to report back to the notifier when an event is complete
	subscriptionJobCompleteChannel chan *SubscriptionJobComplete
	// ctx is the context passed to the go routine.  If the subscription is canceled or the server is stopping the
	// context will be canceled
	ctx context.Context
	// cancel is the CancelFunc associated to the worker context.
	cancel context.CancelFunc
	// workQueue represents the list of work to be done by the worker
	workQueue []*Notification
	// workMutex protects the workQueue from concurrent changes
	workMutex sync.Mutex
	// currentEventDone signals back to the worker that the current event has been processed
	currentEventDone chan *SubscriptionJobComplete
	// client is used to communicate to the subscriber
	client *http.Client
	// logger is used to add info to each log produced by the worker
	logger *slog.Logger
}

// NewSubscriptionWorker creates a new subscription worker object to service a specific subscription
func NewSubscriptionWorker(ctx context.Context, oauthConfig *utils.OAuthClientConfig, subscriptionJobCompleteChannel chan *SubscriptionJobComplete,
	subscription *SubscriptionInfo) (*SubscriptionWorker, error) {
	// Create a client for this subscription.
	client, err := utils.SetupOAuthClient(ctx, oauthConfig)
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
		workChannel:                    make(chan struct{}, 1),
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
	w.workMutex.Lock()
	defer w.workMutex.Unlock()
	w.workQueue = append(w.workQueue, notification)
	w.logger.Debug("notification enqueued to work queue", "size", len(w.workQueue))
	if len(w.workQueue) == 1 {
		// If this is the first entry in the queue, then kick the worker to process its queue; otherwise, let it finish
		// processing the queue before kicking it again.
		w.workChannel <- struct{}{}
	}
}

// Shutdown terminates the worker and releases any pending events
func (w *SubscriptionWorker) Shutdown() {
	w.cancel()
}

// releaseNotifications releases all pending notifications back to the notifier
func (w *SubscriptionWorker) releaseNotifications() {
	for _, notification := range w.workQueue {
		w.subscriptionJobCompleteChannel <- &SubscriptionJobComplete{
			subscriptionID: w.subscription.SubscriptionID,
			notificationID: notification.NotificationID,
			sequenceID:     notification.SequenceID,
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
		case <-w.workChannel:
			w.workMutex.Lock()
			w.processNextEvent(w.ctx, w.workQueue[0])
			w.workMutex.Unlock()
		case <-w.ctx.Done():
			w.releaseNotifications()
			w.logger.Info("subscription worker shutting down")
			return
		}
	}
}

// handleCurrentEventCompletion handles the end of the current event and looks for another event to process.
func (w *SubscriptionWorker) handleCurrentEventCompletion(e *SubscriptionJobComplete) {
	// Forward to the notifier so that it can release it. This may block if the notifier is busy handling other
	// completion jobs or new notifications.
	w.subscriptionJobCompleteChannel <- e

	// dequeue the completed event and handle the next event
	w.workMutex.Lock()
	defer w.workMutex.Unlock()
	w.workQueue = w.workQueue[1:]

	if len(w.workQueue) == 0 {
		// No more events
		w.logger.Debug("no more events to process")
		return
	}
	w.logger.Debug("dequeued notification from work queue", "size", len(w.workQueue))

	w.processNextEvent(w.ctx, w.workQueue[0])
}

// processNextEvent looks for the next event to be processed.
func (w *SubscriptionWorker) processNextEvent(ctx context.Context, nextEvent *Notification) {
	// Launch a task to send the notification (or retry on failures)
	go processEvent(ctx, w.logger, w.client, w.currentEventDone, *nextEvent, w.subscription.SubscriptionID, w.subscription.Callback)
}

/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package async

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

// AsyncEventType defines the types of async events that are supported between the Collector and Reflector.
type AsyncEventType int

const (
	// Updated indicates that the object is inserted or updated
	Updated AsyncEventType = iota
	// Deleted indicates that the object is deleted
	Deleted
	// SyncComplete indicates that the Reflector has re-listed all objects from the API server
	SyncComplete
)

// AsyncChangeEvent defines the data received by a data source to signal that an async handleWatchEvent has been received for a
// given object type.
type AsyncChangeEvent struct {
	DataSourceID uuid.UUID
	EventType    AsyncEventType
	Object       db.Model
	Keys         []uuid.UUID
}

// AsyncEventHandler is intended to be implemented by the Collector so that it can receive data from the Reflector via
// the Store interface.
type AsyncEventHandler interface {
	// HandleSyncComplete is used to indicate that the full list of objects has been received.  Any stale objects should now
	// be deleted.
	HandleSyncComplete(ctx context.Context, objectType runtime.Object, keys []uuid.UUID) error
	// HandleAsyncEvent is used to pass objects received from the Reflector to the Collector
	HandleAsyncEvent(ctx context.Context, obj interface{}, eventType AsyncEventType) (uuid.UUID, error)
}

// operation defines an internal structure used to pass information around about pending operations.
type operation struct {
	eventType AsyncEventType
	// objects is used to store the subject(s) of the operation.  If eventType is Updated or Deleted,
	// then this slice will contain only a single element.  If eventType is SyncComplete, then it
	// may contain 0 or more elements.
	objects []interface{}
}

// ReflectorStore defines an adaptation layer between a Reflector and our Collector.  This is an alternative to using an
// Informer which eliminates the need for caching objects in memory.  On a system engineered to capacity, this can save
// us hundreds of MB of memory.
type ReflectorStore struct {
	// ObjectType is an instance of the object type monitored by the Reflector.
	ObjectType runtime.Object
	// hasSynced indicates whether the initial sync has completed (i.e., whether Replace has been called)
	hasSynced bool
	// queue stores the pending operations received from the Reflector
	queue []operation
	// mutex protects the shared instance variables from concurrency issues
	mutex sync.Mutex
	// ready is used to signal that a new operation has been received from the Reflector
	ready chan struct{}
	// hwm is the high watermark of the queue length
	hwm int
}

// NewReflectorStore creates a new ReflectorStore
func NewReflectorStore(objectType runtime.Object) *ReflectorStore {
	return &ReflectorStore{
		ObjectType: objectType,
		queue:      []operation{},
		ready:      make(chan struct{}, 1),
	}
}

// enqueue adds an operation to the queue and signals the receiver if it is the first operation
// enqueued to the queue.
func (c *ReflectorStore) enqueue(operation operation) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.queue = append(c.queue, operation)
	if operation.eventType == SyncComplete {
		c.hasSynced = true
	}
	count := len(c.queue)
	if count == 1 {
		c.ready <- struct{}{}
	}
	if count >= c.hwm+10 {
		// We don't need a log every single time the high watermark is surpassed by 1, so log it when it surpasses by 10
		slog.Debug("New reflector store queue high watermark", "new", count, "old", c.hwm)
		c.hwm = count
	}
}

// handleOperations processes the operations that have been enqueued by the Store interface.
func (c *ReflectorStore) handleOperations(ctx context.Context, handler AsyncEventHandler) {
	c.mutex.Lock()
	// Move the operations into a local variable so that we minimize how long we hold the lock to
	// avoid blocking the Reflector
	operations := c.queue
	c.queue = make([]operation, 0)
	c.mutex.Unlock()

	for _, o := range operations {
		switch o.eventType {
		case Updated, Deleted:
			_, err := handler.HandleAsyncEvent(ctx, o.objects[0], o.eventType)
			if err != nil {
				slog.Warn("Failed to handle event", "event", o.eventType, "error", err)
			}
		case SyncComplete:
			keys := make([]uuid.UUID, 0)
			for _, obj := range o.objects {
				key, err := handler.HandleAsyncEvent(ctx, obj, Updated)
				if err != nil {
					slog.Warn("Failed to handle event", "error", err)
					continue
				}
				if key != uuid.Nil {
					// Some may have been filtered out.  No need to track those.
					keys = append(keys, key)
				}
			}
			err := handler.HandleSyncComplete(ctx, c.ObjectType, keys)
			if err != nil {
				slog.Warn("Failed to handle sync completion", "error", err)
			}
		}
	}
}

// Receive waits for new operations to be enqueued to the work queue and then invokes the handler
// to process each pending operation.  This method does not return unless the context is canceled.
func (c *ReflectorStore) Receive(ctx context.Context, handler AsyncEventHandler) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("stopping store adapter; context canceled")
			close(c.ready)
			return
		case <-c.ready:
			c.handleOperations(ctx, handler)
		}
	}
}

// HasSynced is used to determine whether the initial sync operation has completed, which means the initial set of
// objects has been retrieved from the API server.
func (c *ReflectorStore) HasSynced() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.hasSynced
}

// Add handles a new object being added.
func (c *ReflectorStore) Add(obj interface{}) error {
	c.enqueue(operation{
		eventType: Updated,
		objects:   []interface{}{obj},
	})
	return nil
}

// Update handles an update to an object.
func (c *ReflectorStore) Update(obj interface{}) error {
	c.enqueue(operation{
		eventType: Updated,
		objects:   []interface{}{obj},
	})
	return nil
}

// Delete handles a deleted object
func (c *ReflectorStore) Delete(obj interface{}) error {
	c.enqueue(operation{
		eventType: Deleted,
		objects:   []interface{}{obj},
	})
	return nil
}

// List is not supported.  We only need this interface to accept incoming data via Add, Update, Delete, and Replace.
func (c *ReflectorStore) List() []interface{} {
	// The Reflector does not use this method so it is not required.
	panic("not supported")
}

// ListKeys is not supported.  We only need this interface to accept incoming data via Add, Update, Delete, and Replace.
func (c *ReflectorStore) ListKeys() []string {
	// The Reflector does not use this method so it is not required.
	panic("not supported")
}

// Get is not supported.  We only need this interface to accept incoming data via Add, Update, Delete, and Replace.
func (c *ReflectorStore) Get(_ interface{}) (item interface{}, exists bool, err error) {
	// The Reflector does not use this method so it is not required.
	panic("not supported")
}

// GetByKey is not supported.  We only need this interface to accept incoming data via Add, Update, Delete, and Replace.
func (c *ReflectorStore) GetByKey(_ string) (item interface{}, exists bool, err error) {
	// The Reflector does not use this method so it is not required.
	panic("not supported")
}

// Replace indicates that the underlying Reflector needed to re-list all data from the API server.
func (c *ReflectorStore) Replace(items []interface{}, resourceVersion string) error {
	slog.Info("Replace called", "type", fmt.Sprintf("%T", c.ObjectType),
		"items", len(items), "resourceVersion", resourceVersion)
	c.enqueue(operation{
		eventType: SyncComplete,
		objects:   items,
	})
	return nil
}

// Resync indicates that a resync operation has occurred.  We do not use this interface.
func (c *ReflectorStore) Resync() error {
	// We don't have resync enabled, so this should not be called.
	slog.Info("Resync called")
	return nil
}

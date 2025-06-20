/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package async

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// testObject is a simple test object that implements runtime.Object
type testObject struct {
	Name string
}

func (t *testObject) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (t *testObject) DeepCopyObject() runtime.Object {
	return &testObject{Name: t.Name}
}

var _ = Describe("ReflectorStore", func() {
	var (
		ctrl        *gomock.Controller
		mockHandler *MockAsyncEventHandler
		store       *ReflectorStore
		ctx         context.Context
		cancel      context.CancelFunc
		objectType  runtime.Object
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockHandler = NewMockAsyncEventHandler(ctrl)
		objectType = &testObject{}
		store = NewReflectorStore(objectType)
		ctx, cancel = context.WithCancel(context.Background())
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Describe("NewReflectorStore", func() {
		It("should create a new ReflectorStore with correct initial state", func() {
			newStore := NewReflectorStore(objectType)
			Expect(newStore).NotTo(BeNil())
			Expect(newStore.ObjectType).To(Equal(objectType))
			Expect(newStore.HasSynced()).To(BeFalse())
			Expect(newStore.queue).To(HaveLen(0))
			Expect(newStore.ready).NotTo(BeNil())
		})
	})

	Describe("HasSynced", func() {
		It("should return false initially", func() {
			Expect(store.HasSynced()).To(BeFalse())
		})

		It("should return true after Replace is called", func() {
			err := store.Replace([]interface{}{}, "version1")
			Expect(err).NotTo(HaveOccurred())
			Expect(store.HasSynced()).To(BeTrue())
		})
	})

	Describe("Add", func() {
		It("should successfully add an object", func() {
			obj := &testObject{Name: "test1"}
			err := store.Add(obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should enqueue an Updated operation", func() {
			obj := &testObject{Name: "test1"}
			err := store.Add(obj)
			Expect(err).NotTo(HaveOccurred())

			store.mutex.Lock()
			Expect(store.queue).To(HaveLen(1))
			Expect(store.queue[0].eventType).To(Equal(Updated))
			Expect(store.queue[0].objects).To(HaveLen(1))
			Expect(store.queue[0].objects[0]).To(Equal(obj))
			store.mutex.Unlock()
		})
	})

	Describe("Update", func() {
		It("should successfully update an object", func() {
			obj := &testObject{Name: "test1"}
			err := store.Update(obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should enqueue an Updated operation", func() {
			obj := &testObject{Name: "test1"}
			err := store.Update(obj)
			Expect(err).NotTo(HaveOccurred())

			store.mutex.Lock()
			Expect(store.queue).To(HaveLen(1))
			Expect(store.queue[0].eventType).To(Equal(Updated))
			Expect(store.queue[0].objects).To(HaveLen(1))
			Expect(store.queue[0].objects[0]).To(Equal(obj))
			store.mutex.Unlock()
		})
	})

	Describe("Delete", func() {
		It("should successfully delete an object", func() {
			obj := &testObject{Name: "test1"}
			err := store.Delete(obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should enqueue a Deleted operation", func() {
			obj := &testObject{Name: "test1"}
			err := store.Delete(obj)
			Expect(err).NotTo(HaveOccurred())

			store.mutex.Lock()
			Expect(store.queue).To(HaveLen(1))
			Expect(store.queue[0].eventType).To(Equal(Deleted))
			Expect(store.queue[0].objects).To(HaveLen(1))
			Expect(store.queue[0].objects[0]).To(Equal(obj))
			store.mutex.Unlock()
		})
	})

	Describe("Replace", func() {
		It("should successfully replace with empty list", func() {
			err := store.Replace([]interface{}{}, "version1")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should successfully replace with objects", func() {
			objects := []interface{}{
				&testObject{Name: "obj1"},
				&testObject{Name: "obj2"},
			}
			err := store.Replace(objects, "version1")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should enqueue a SyncComplete operation and set hasSynced", func() {
			objects := []interface{}{
				&testObject{Name: "obj1"},
				&testObject{Name: "obj2"},
			}
			err := store.Replace(objects, "version1")
			Expect(err).NotTo(HaveOccurred())

			store.mutex.Lock()
			Expect(store.queue).To(HaveLen(1))
			Expect(store.queue[0].eventType).To(Equal(SyncComplete))
			Expect(store.queue[0].objects).To(HaveLen(2))
			Expect(store.hasSynced).To(BeTrue())
			store.mutex.Unlock()
		})
	})

	Describe("Resync", func() {
		It("should return nil without error", func() {
			err := store.Resync()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Receive", func() {
		Context("when handling Updated events", func() {
			It("should call HandleAsyncEvent for Added objects", func() {
				obj := &testObject{Name: "test1"}
				testUUID := uuid.New()

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj, Updated).
					Return(testUUID, nil).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Add an object
				err := store.Add(obj)
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})

			It("should call HandleAsyncEvent for Updated objects", func() {
				obj := &testObject{Name: "test1"}
				testUUID := uuid.New()

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj, Updated).
					Return(testUUID, nil).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Update an object
				err := store.Update(obj)
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})

			It("should handle errors from HandleAsyncEvent gracefully", func() {
				obj := &testObject{Name: "test1"}

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj, Updated).
					Return(uuid.Nil, errors.New("handler error")).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Add an object
				err := store.Add(obj)
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})
		})

		Context("when handling Deleted events", func() {
			It("should call HandleAsyncEvent for Deleted objects", func() {
				obj := &testObject{Name: "test1"}
				testUUID := uuid.New()

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj, Deleted).
					Return(testUUID, nil).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Delete an object
				err := store.Delete(obj)
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})
		})

		Context("when handling SyncComplete events", func() {
			It("should call HandleAsyncEvent for each object and then HandleSyncComplete", func() {
				obj1 := &testObject{Name: "obj1"}
				obj2 := &testObject{Name: "obj2"}
				objects := []interface{}{obj1, obj2}

				uuid1 := uuid.New()
				uuid2 := uuid.New()
				expectedKeys := []uuid.UUID{uuid1, uuid2}

				// Expect HandleAsyncEvent for each object
				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj1, Updated).
					Return(uuid1, nil).
					Times(1)

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj2, Updated).
					Return(uuid2, nil).
					Times(1)

				// Expect HandleSyncComplete with the keys
				mockHandler.EXPECT().
					HandleSyncComplete(gomock.Any(), objectType, expectedKeys).
					Return(nil).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Replace objects
				err := store.Replace(objects, "version1")
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})

			It("should filter out nil UUIDs from HandleSyncComplete", func() {
				obj1 := &testObject{Name: "obj1"}
				obj2 := &testObject{Name: "obj2"}
				objects := []interface{}{obj1, obj2}

				uuid1 := uuid.New()
				expectedKeys := []uuid.UUID{uuid1} // Only uuid1 should be included

				// First object returns valid UUID, second returns Nil
				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj1, Updated).
					Return(uuid1, nil).
					Times(1)

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj2, Updated).
					Return(uuid.Nil, nil). // This should be filtered out
					Times(1)

				// Expect HandleSyncComplete with only the valid key
				mockHandler.EXPECT().
					HandleSyncComplete(gomock.Any(), objectType, expectedKeys).
					Return(nil).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Replace objects
				err := store.Replace(objects, "version1")
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})

			It("should handle errors from HandleAsyncEvent during SyncComplete", func() {
				obj1 := &testObject{Name: "obj1"}
				obj2 := &testObject{Name: "obj2"}
				objects := []interface{}{obj1, obj2}

				uuid2 := uuid.New()
				expectedKeys := []uuid.UUID{uuid2} // Only uuid2 should be included

				// First object returns error, second returns valid UUID
				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj1, Updated).
					Return(uuid.Nil, errors.New("handler error")).
					Times(1)

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), obj2, Updated).
					Return(uuid2, nil).
					Times(1)

				// Expect HandleSyncComplete with only the successful key
				mockHandler.EXPECT().
					HandleSyncComplete(gomock.Any(), objectType, expectedKeys).
					Return(nil).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Replace objects
				err := store.Replace(objects, "version1")
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})

			It("should handle errors from HandleSyncComplete gracefully", func() {
				objects := []interface{}{&testObject{Name: "obj1"}}
				uuid1 := uuid.New()

				mockHandler.EXPECT().
					HandleAsyncEvent(gomock.Any(), gomock.Any(), Updated).
					Return(uuid1, nil).
					Times(1)

				mockHandler.EXPECT().
					HandleSyncComplete(gomock.Any(), objectType, []uuid.UUID{uuid1}).
					Return(errors.New("sync complete error")).
					Times(1)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Replace objects
				err := store.Replace(objects, "version1")
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})
		})

		Context("when context is canceled", func() {
			It("should stop processing and close the ready channel", func() {
				// Start Receive in a goroutine
				done := make(chan bool)
				go func() {
					store.Receive(ctx, mockHandler)
					done <- true
				}()

				// Cancel the context
				cancel()

				// Wait for Receive to finish
				Eventually(done, time.Second).Should(Receive())
			})
		})

		Context("when processing multiple operations", func() {
			It("should process all enqueued operations in order", func() {
				obj1 := &testObject{Name: "obj1"}
				obj2 := &testObject{Name: "obj2"}
				obj3 := &testObject{Name: "obj3"}

				uuid1 := uuid.New()
				uuid2 := uuid.New()

				// Expect calls in order
				gomock.InOrder(
					mockHandler.EXPECT().
						HandleAsyncEvent(gomock.Any(), obj1, Updated).
						Return(uuid1, nil),
					mockHandler.EXPECT().
						HandleAsyncEvent(gomock.Any(), obj2, Deleted).
						Return(uuid2, nil),
					mockHandler.EXPECT().
						HandleAsyncEvent(gomock.Any(), obj3, Updated).
						Return(uuid.New(), nil),
				)

				// Start Receive in a goroutine
				go store.Receive(ctx, mockHandler)

				// Add multiple operations
				err := store.Add(obj1)
				Expect(err).NotTo(HaveOccurred())

				err = store.Delete(obj2)
				Expect(err).NotTo(HaveOccurred())

				err = store.Update(obj3)
				Expect(err).NotTo(HaveOccurred())

				// Give some time for processing
				time.Sleep(100 * time.Millisecond)
				cancel()
			})
		})
	})

	Describe("Unsupported methods", func() {
		It("should panic on List", func() {
			Expect(func() { store.List() }).To(Panic())
		})

		It("should panic on ListKeys", func() {
			Expect(func() { store.ListKeys() }).To(Panic())
		})

		It("should panic on Get", func() {
			Expect(func() {
				_, _, err := store.Get(nil)
				Expect(err).ToNot(HaveOccurred())
			}).To(Panic())
		})

		It("should panic on GetByKey", func() {
			Expect(func() {
				_, _, err := store.GetByKey("test")
				Expect(err).ToNot(HaveOccurred())
			}).To(Panic())
		})
	})
})

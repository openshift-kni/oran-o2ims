/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	inventoryv1alpha1 "github.com/openshift-kni/oran-o2ims/api/inventory/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	hwmgrcontroller "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = metal3v1alpha1.AddToScheme(scheme)
	_ = hwmgmtv1alpha1.AddToScheme(scheme)
	_ = inventoryv1alpha1.AddToScheme(scheme)
	return scheme
}

const testNamespace = "test-ns"

func newTestBMH(name, poolName string, state metal3v1alpha1.ProvisioningState) *metal3v1alpha1.BareMetalHost {
	bmh := &metal3v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			UID:       types.UID(uuid.NewSHA1(uuid.Nil, []byte(testNamespace+"/"+name)).String()),
			Labels: map[string]string{
				constants.LabelResourcePoolName: poolName,
			},
		},
		Spec: metal3v1alpha1.BareMetalHostSpec{
			Online: true,
		},
	}
	bmh.Status.Provisioning.State = state
	return bmh
}

func newTestHardwareData(name string) *metal3v1alpha1.HardwareData {
	return &metal3v1alpha1.HardwareData{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: metal3v1alpha1.HardwareDataSpec{
			HardwareDetails: &metal3v1alpha1.HardwareDetails{
				SystemVendor: metal3v1alpha1.HardwareSystemVendor{
					Manufacturer: "Dell",
					ProductName:  "R640",
					SerialNumber: "SN123",
				},
				RAMMebibytes: 16384,
				CPU: metal3v1alpha1.CPU{
					Arch:  "x86_64",
					Count: 32,
				},
			},
		},
	}
}

func newTestResourcePool(name string) *inventoryv1alpha1.ResourcePool {
	return &inventoryv1alpha1.ResourcePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "oran-o2ims",
			UID:       types.UID(uuid.NewSHA1(uuid.Nil, []byte("pool/"+name)).String()),
		},
		Status: inventoryv1alpha1.ResourcePoolStatus{
			Conditions: []metav1.Condition{{
				Type:   inventoryv1alpha1.ConditionTypeReady,
				Status: metav1.ConditionTrue,
			}},
		},
	}
}

var _ = Describe("HardwareDataSource", func() {
	var (
		ds      *HardwareDataSource
		cloudID uuid.UUID
	)

	BeforeEach(func() {
		cloudID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
		ds = &HardwareDataSource{
			cloudID: cloudID,
		}
	})

	Describe("Init / generation / identity", func() {
		It("records data source id, generation, and channel from Init", func() {
			id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
			ch := make(chan *async.AsyncChangeEvent, 1)
			ds.Init(id, 3, ch)
			Expect(ds.GetID()).To(Equal(id))
			Expect(ds.GetGenerationID()).To(Equal(3))
			Expect(ds.Name()).To(Equal("HardwareDataSource"))
			Expect(ds.AsyncChangeEvents).NotTo(BeNil())
		})

		It("updates generation via SetGenerationID and IncrGenerationID", func() {
			ds.Init(uuid.New(), 10, nil)
			ds.SetGenerationID(20)
			Expect(ds.GetGenerationID()).To(Equal(20))
			Expect(ds.IncrGenerationID()).To(Equal(21))
			Expect(ds.GetGenerationID()).To(Equal(21))
		})
	})

	Describe("convertResource", func() {
		BeforeEach(func() {
			ds.Init(uuid.MustParse("33333333-3333-3333-3333-333333333333"), 1, nil)
		})

		It("maps a minimal ResourceInfo to models.Resource", func() {
			poolID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
			resID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
			in := hwmgrcontroller.ResourceInfo{
				ResourceId:       resID,
				ResourcePoolId:   poolID,
				Description:      "node-a",
				Vendor:           "Dell",
				Model:            "R640",
				Memory:           16384,
				AdminState:       hwmgrcontroller.ResourceInfoAdminStateUNLOCKED,
				OperationalState: hwmgrcontroller.ResourceInfoOperationalStateENABLED,
				UsageState:       hwmgrcontroller.ACTIVE,
				HwProfile:        "profile-1",
				Processors:       []hwmgrcontroller.ProcessorInfo{},
			}
			out := ds.convertResource(&in)
			Expect(out.ResourceID).To(Equal(resID))
			Expect(out.ResourcePoolID).To(Equal(poolID))
			Expect(out.Description).To(Equal("node-a"))
			Expect(out.Extensions[vendorExtension]).To(Equal("Dell"))
			Expect(out.Extensions[modelExtension]).To(Equal("R640"))
			Expect(out.Extensions[memoryExtension]).To(Equal("16384 MiB"))
			Expect(out.Extensions[hwProfileExtension]).To(Equal("profile-1"))
			Expect(out.ExternalID).To(Equal(resID.String()))
		})

		It("includes optional power, labels, allocated, nics, and storage extensions", func() {
			resID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
			poolID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
			ps := hwmgrcontroller.ON
			allocated := true
			labels := map[string]string{"k": "v"}
			nics := map[string]hwmgrcontroller.NicInfo{"eth0": {Mac: ptr("00:11:22:33:44:55")}}
			storage := map[string]hwmgrcontroller.StorageInfo{"sda": {SizeBytes: ptrInt64(1024)}}
			in := hwmgrcontroller.ResourceInfo{
				ResourceId:       resID,
				ResourcePoolId:   poolID,
				Description:      "full",
				Vendor:           "Vendor",
				Model:            "Model",
				Memory:           4096,
				AdminState:       hwmgrcontroller.ResourceInfoAdminStateLOCKED,
				OperationalState: hwmgrcontroller.ResourceInfoOperationalStateDISABLED,
				UsageState:       hwmgrcontroller.BUSY,
				HwProfile:        "p",
				Processors:       []hwmgrcontroller.ProcessorInfo{{}},
				PowerState:       &ps,
				Labels:           &labels,
				Allocated:        &allocated,
				Nics:             &nics,
				Storage:          &storage,
			}
			out := ds.convertResource(&in)
			Expect(out.Extensions[powerStateExtension]).To(Equal(string(hwmgrcontroller.ON)))
			Expect(out.Extensions[labelsExtension]).To(Equal(labels))
			Expect(out.Extensions[allocatedExtension]).To(BeTrue())
			Expect(out.Extensions[nicsExtension]).To(Equal(nics))
			Expect(out.Extensions[storageExtension]).To(Equal(storage))
		})
	})

	Describe("MakeResourceType", func() {
		BeforeEach(func() {
			ds.Init(uuid.MustParse("88888888-8888-8888-8888-888888888888"), 2, nil)
		})

		It("builds a resource type from resource extensions", func() {
			res := &models.Resource{
				Extensions: map[string]any{
					vendorExtension: "Acme",
					modelExtension:  "Box",
				},
			}
			rt, err := ds.MakeResourceType(res)
			Expect(err).NotTo(HaveOccurred())
			Expect(rt.Name).To(Equal("Acme/Box"))
			Expect(rt.Vendor).To(Equal("Acme"))
			Expect(rt.Model).To(Equal("Box"))
			Expect(rt.ResourceKind).To(Equal(models.ResourceKindPhysical))
			Expect(rt.ResourceClass).To(Equal(models.ResourceClassCompute))
			Expect(rt.GenerationID).To(Equal(2))
		})
	})

	Describe("HandleAsyncEvent", func() {
		var (
			ctx     context.Context
			eventCh chan *async.AsyncChangeEvent
		)

		BeforeEach(func() {
			ctx = context.Background()
			eventCh = make(chan *async.AsyncChangeEvent, 20)
			scheme := newTestScheme()
			pool := newTestResourcePool("test-pool")
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
			ds.hubClient = fakeClient
			ds.Init(uuid.MustParse("99999999-9999-9999-9999-999999999999"), 1, eventCh)
		})

		It("sends ResourceType and Resource events for a valid BMH", func() {
			bmh := newTestBMH("bmh-1", "test-pool", metal3v1alpha1.StateAvailable)
			hwdata := newTestHardwareData("bmh-1")

			scheme := newTestScheme()
			pool := newTestResourcePool("test-pool")
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, hwdata, pool).Build()
			ds.hubClient = fakeClient

			key, err := ds.HandleAsyncEvent(ctx, bmh, async.Updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).NotTo(Equal(uuid.Nil))

			Expect(eventCh).To(HaveLen(2))
			rtEvent := <-eventCh
			Expect(rtEvent.EventType).To(Equal(async.Updated))
			_, isRT := rtEvent.Object.(models.ResourceType)
			Expect(isRT).To(BeTrue())

			resEvent := <-eventCh
			Expect(resEvent.EventType).To(Equal(async.Updated))
			res, isRes := resEvent.Object.(models.Resource)
			Expect(isRes).To(BeTrue())
			Expect(res.Extensions[vendorExtension]).To(Equal("Dell"))
		})

		It("sends delete event for a BMH not included in inventory", func() {
			bmh := newTestBMH("bmh-del", "test-pool", metal3v1alpha1.StateRegistering)

			key, err := ds.HandleAsyncEvent(ctx, bmh, async.Updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).NotTo(Equal(uuid.Nil))

			Expect(eventCh).To(HaveLen(1))
			event := <-eventCh
			Expect(event.EventType).To(Equal(async.Deleted))
		})

		It("sends delete event for a deleted BMH", func() {
			bmh := newTestBMH("bmh-gone", "test-pool", metal3v1alpha1.StateAvailable)

			key, err := ds.HandleAsyncEvent(ctx, bmh, async.Deleted)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).NotTo(Equal(uuid.Nil))

			Expect(eventCh).To(HaveLen(1))
			event := <-eventCh
			Expect(event.EventType).To(Equal(async.Deleted))
		})

		It("rebuilds resource from HardwareData event", func() {
			bmh := newTestBMH("hwdata-bmh", "test-pool", metal3v1alpha1.StateAvailable)
			hwdata := newTestHardwareData("hwdata-bmh")
			pool := newTestResourcePool("test-pool")

			scheme := newTestScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, hwdata, pool).Build()
			ds.hubClient = fakeClient

			key, err := ds.HandleAsyncEvent(ctx, hwdata, async.Updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).NotTo(Equal(uuid.Nil))
			Expect(eventCh).To(HaveLen(2))
		})

		It("skips HardwareData event when BMH not found", func() {
			hwdata := newTestHardwareData("no-bmh")

			key, err := ds.HandleAsyncEvent(ctx, hwdata, async.Updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).To(Equal(uuid.Nil))
			Expect(eventCh).To(BeEmpty())
		})

		It("rebuilds resource from AllocatedNode event", func() {
			bmh := newTestBMH("an-bmh", "test-pool", metal3v1alpha1.StateAvailable)
			hwdata := newTestHardwareData("an-bmh")
			pool := newTestResourcePool("test-pool")
			node := &hwmgmtv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node",
					Namespace: testNamespace,
				},
				Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
					HwMgrNodeId: "an-bmh",
					HwMgrNodeNs: testNamespace,
				},
			}

			scheme := newTestScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, hwdata, pool, node).Build()
			ds.hubClient = fakeClient

			key, err := ds.HandleAsyncEvent(ctx, node, async.Updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).NotTo(Equal(uuid.Nil))
			Expect(eventCh).To(HaveLen(2))
		})

		It("skips AllocatedNode event when BMH not found", func() {
			node := &hwmgmtv1alpha1.AllocatedNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orphan-node",
					Namespace: testNamespace,
				},
				Spec: hwmgmtv1alpha1.AllocatedNodeSpec{
					HwMgrNodeId: "missing-bmh",
					HwMgrNodeNs: testNamespace,
				},
			}

			key, err := ds.HandleAsyncEvent(ctx, node, async.Updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).To(Equal(uuid.Nil))
			Expect(eventCh).To(BeEmpty())
		})

		It("returns error for unknown object type", func() {
			_, err := ds.HandleAsyncEvent(ctx, "not-a-k8s-object", async.Updated)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("HandleSyncComplete", func() {
		var (
			ctx     context.Context
			eventCh chan *async.AsyncChangeEvent
		)

		BeforeEach(func() {
			ctx = context.Background()
			eventCh = make(chan *async.AsyncChangeEvent, 5)
			ds.Init(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), 1, eventCh)
		})

		It("sends SyncComplete event for BMH objectType", func() {
			keys := []uuid.UUID{uuid.New(), uuid.New()}
			err := ds.HandleSyncComplete(ctx, &metal3v1alpha1.BareMetalHost{}, keys)
			Expect(err).NotTo(HaveOccurred())

			Expect(eventCh).To(HaveLen(1))
			event := <-eventCh
			Expect(event.EventType).To(Equal(async.SyncComplete))
			_, isResource := event.Object.(models.Resource)
			Expect(isResource).To(BeTrue())
			Expect(event.Keys).To(Equal(keys))
		})

		It("does not send event for HardwareData objectType", func() {
			err := ds.HandleSyncComplete(ctx, &metal3v1alpha1.HardwareData{}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventCh).To(BeEmpty())
		})

		It("does not send event for AllocatedNode objectType", func() {
			err := ds.HandleSyncComplete(ctx, &hwmgmtv1alpha1.AllocatedNode{}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventCh).To(BeEmpty())
		})
	})

	Describe("BuildResourcesForPool", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
			ds.Init(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), 1, nil)
		})

		It("returns resources for BMHs in the given pool", func() {
			bmh := newTestBMH("pool-bmh", "my-pool", metal3v1alpha1.StateAvailable)
			hwdata := newTestHardwareData("pool-bmh")
			pool := newTestResourcePool("my-pool")

			scheme := newTestScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, hwdata, pool).Build()
			ds.hubClient = fakeClient

			results, err := ds.BuildResourcesForPool(ctx, "my-pool")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Resource.Extensions[vendorExtension]).To(Equal("Dell"))
			Expect(results[0].ResourceType.Vendor).To(Equal("Dell"))
		})

		It("returns empty when no BMHs reference the pool", func() {
			pool := newTestResourcePool("empty-pool")
			scheme := newTestScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
			ds.hubClient = fakeClient

			results, err := ds.BuildResourcesForPool(ctx, "empty-pool")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("skips BMHs not included in inventory", func() {
			bmh := newTestBMH("bad-state", "my-pool", metal3v1alpha1.StateRegistering)
			pool := newTestResourcePool("my-pool")

			scheme := newTestScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, pool).Build()
			ds.hubClient = fakeClient

			results, err := ds.BuildResourcesForPool(ctx, "my-pool")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})
	})
})

func ptr(s string) *string { return &s }

func ptrInt64(v int64) *int64 { return &v }

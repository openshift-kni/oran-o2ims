/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metal3controller "github.com/openshift-kni/oran-o2ims/hwmgr-plugins/metal3/controller"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

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
		It("records data source id and generation from Init", func() {
			id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
			ch := make(chan *async.AsyncChangeEvent, 1)
			ds.Init(id, 3, ch)
			Expect(ds.GetID()).To(Equal(id))
			Expect(ds.GetGenerationID()).To(Equal(3))
			Expect(ds.Name()).To(Equal("HardwareDataSource"))
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
			in := metal3controller.ResourceInfo{
				ResourceId:       resID,
				ResourcePoolId:   poolID,
				Description:      "node-a",
				Vendor:           "Dell",
				Model:            "R640",
				Memory:           16384,
				AdminState:       metal3controller.ResourceInfoAdminStateUNLOCKED,
				OperationalState: metal3controller.ResourceInfoOperationalStateENABLED,
				UsageState:       metal3controller.ACTIVE,
				HwProfile:        "profile-1",
				Processors:       []metal3controller.ProcessorInfo{},
			}
			out := ds.convertResource(&in)
			Expect(out.ResourceID).To(Equal(resID))
			Expect(out.ResourcePoolID).To(Equal(poolID))
			Expect(out.Description).To(Equal("node-a"))
			Expect(out.Extensions[vendorExtension]).To(Equal("Dell"))
			Expect(out.Extensions[modelExtension]).To(Equal("R640"))
			Expect(out.Extensions[memoryExtension]).To(Equal("16384 MiB"))
			Expect(out.Extensions[hwProfileExtension]).To(Equal("profile-1"))
			Expect(out.ExternalID).To(Equal(hardwareDataSourceName + "/" + resID.String()))
		})

		It("includes optional power, labels, allocated, nics, and storage extensions", func() {
			resID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
			poolID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
			ps := metal3controller.ON
			allocated := true
			labels := map[string]string{"k": "v"}
			nics := map[string]metal3controller.NicInfo{"eth0": {Mac: ptr("00:11:22:33:44:55")}}
			storage := map[string]metal3controller.StorageInfo{"sda": {SizeBytes: ptrInt64(1024)}}
			in := metal3controller.ResourceInfo{
				ResourceId:       resID,
				ResourcePoolId:   poolID,
				Description:      "full",
				Vendor:           "Vendor",
				Model:            "Model",
				Memory:           4096,
				AdminState:       metal3controller.ResourceInfoAdminStateLOCKED,
				OperationalState: metal3controller.ResourceInfoOperationalStateDISABLED,
				UsageState:       metal3controller.BUSY,
				HwProfile:        "p",
				Processors:       []metal3controller.ProcessorInfo{{}},
				PowerState:       &ps,
				Labels:           &labels,
				Allocated:        &allocated,
				Nics:             &nics,
				Storage:          &storage,
			}
			out := ds.convertResource(&in)
			Expect(out.Extensions[powerStateExtension]).To(Equal(string(metal3controller.ON)))
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
})

func ptr(s string) *string { return &s }

func ptrInt64(v int64) *int64 { return &v }

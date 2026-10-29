/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgrcontroller "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

type staticGPUInventoryReader struct {
	inventory *hwmgrcontroller.GPUInventory
	err       error
}

type countingGPUInventoryReader struct {
	calls     int
	inventory *hwmgrcontroller.GPUInventory
}

type staticGPUInventoryResourceReader struct {
	calls    int
	resource *models.Resource
	err      error
}

func (r *staticGPUInventoryResourceReader) GetResource(context.Context, uuid.UUID) (*models.Resource, error) {
	r.calls++
	return r.resource, r.err
}

func (r staticGPUInventoryReader) GetGPUInventory(
	context.Context,
	*metal3v1alpha1.BareMetalHost,
) (*hwmgrcontroller.GPUInventory, error) {
	return r.inventory, r.err
}

func (r *countingGPUInventoryReader) GetGPUInventory(
	context.Context,
	*metal3v1alpha1.BareMetalHost,
) (*hwmgrcontroller.GPUInventory, error) {
	r.calls++
	return r.inventory, nil
}

var _ = Describe("Redfish GPU inventory", func() {
	var (
		ctx       context.Context
		bmh       *metal3v1alpha1.BareMetalHost
		reader    *redfishGPUInventoryClient
		handler   http.Handler
		requestOK bool
	)

	BeforeEach(func() {
		ctx = context.Background()
		requestOK = true
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "bmc-secret", Namespace: testNamespace},
			Data:       map[string][]byte{"username": []byte("reader"), "password": []byte("secret")},
		}
		hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		reader = newRedfishGPUInventoryClient(hubClient)
		bmh = newTestBMH("gpu-host", "pool", metal3v1alpha1.StateAvailable)
		bmh.Spec.BMC.CredentialsName = "bmc-secret"
	})

	It("preserves discovered GPUs when another Redfish PCIe device query fails", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if username, password, ok := r.BasicAuth(); !ok || username != "reader" || password != "secret" {
				requestOK = false
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"},"Chassis":{"@odata.id":"/redfish/v1/Chassis"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Id":"1","PCIeDevices":{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices"}}`))
			case "/redfish/v1/Systems/1/PCIeDevices":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1"},{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/Broken1"}]}`))
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1":
				_, _ = w.Write([]byte(`{"Id":"GPU1","Manufacturer":"NVIDIA","Model":"H100","SerialNumber":"GPU-SN-1","PCIeFunctions":{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions"},"Slot":{"Location":{"PartLocation":{"ServiceLabel":"RISER_SLOT_1"}}},"Status":{"State":"Enabled","Health":"OK"}}`))
			case "/redfish/v1/Systems/1/PCIeDevices/Broken1":
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions/0"}]}`))
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions/0":
				_, _ = w.Write([]byte(`{"DeviceClass":"ProcessingAccelerators","BusNumber":"0x01","DeviceNumber":"0x00","FunctionNumber":"0x0","SegmentNumber":"0x0"}`))
			case "/redfish/v1/Chassis":
				_, _ = w.Write([]byte(`{"Members":[]}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "idrac-virtualmedia+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(requestOK).To(BeTrue())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusPartial))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0].Manufacturer).To(Equal("NVIDIA"))
		Expect(inventory.Devices[0].Model).To(Equal("H100"))
		Expect(inventory.Devices[0].Location).To(Equal("RISER_SLOT_1"))
		Expect(inventory.Devices[0].PCIAddress).To(Equal("0000:01:00.0"))
	})

	It("discovers GPUs through the system-level PCIe functions collection", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"PCIeFunctions":{"@odata.id":"/redfish/v1/Systems/1/PCIeFunctions"}}`))
			case "/redfish/v1/Systems/1/PCIeFunctions":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/PCIeFunctions/0"}]}`))
			case "/redfish/v1/Systems/1/PCIeFunctions/0":
				_, _ = w.Write([]byte(`{"Id":"GPU1-function0","DeviceClass":"ProcessingAccelerators","BusNumber":"0x01","DeviceNumber":"0x00","FunctionId":0,"Links":{"PCIeDevice":{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1"}}}`))
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1":
				_, _ = w.Write([]byte(`{"Id":"GPU1","Manufacturer":"NVIDIA","Model":"H100","SerialNumber":"GPU-SN-1","Status":{"State":"Enabled","Health":"OK"}}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0].Model).To(Equal("H100"))
		Expect(inventory.Devices[0].PCIAddress).To(Equal("0000:01:00.0"))
	})

	It("discovers GPUs when the ComputerSystem PCIe properties are arrays of links", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Id":"1","PCIeDevices":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1"}]}`))
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1":
				_, _ = w.Write([]byte(`{"Id":"GPU1","Manufacturer":"NVIDIA","Model":"H100","SerialNumber":"GPU-SN-1","PCIeFunctions":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions/0"}],"Slot":{"Location":{"PartLocation":{"ServiceLabel":"RISER_SLOT_1"}}},"Status":{"State":"Enabled","Health":"OK"}}`))
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions/0":
				_, _ = w.Write([]byte(`{"DeviceClass":"ProcessingAccelerators","BusNumber":"0x01","DeviceNumber":"0x00","FunctionNumber":"0x0","SegmentNumber":"0x0"}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0].Model).To(Equal("H100"))
		Expect(inventory.Devices[0].Location).To(Equal("RISER_SLOT_1"))
		Expect(inventory.Devices[0].PCIAddress).To(Equal("0000:01:00.0"))
	})

	It("reports a successful empty inventory when the ComputerSystem PCIe arrays are empty", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Id":"1","PCIeDevices":[],"PCIeFunctions":[]}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusUnsupported))
		Expect(inventory.Devices).To(BeEmpty())
	})

	It("scopes chassis discovery to the chassis linked from the selected system", func() {
		serviceWideChassisTraversed := false
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"},"Chassis":{"@odata.id":"/redfish/v1/Chassis"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Id":"1","Links":{"Chassis":[{"@odata.id":"/redfish/v1/Chassis/1"}]}}`))
			case "/redfish/v1/Chassis/1":
				_, _ = w.Write([]byte(`{"Id":"1","PCIeDevices":[{"@odata.id":"/redfish/v1/Chassis/1/PCIeDevices/NIC1"}]}`))
			case "/redfish/v1/Chassis/1/PCIeDevices/NIC1":
				_, _ = w.Write([]byte(`{"Id":"NIC1","Model":"Ethernet Adapter","PCIeFunctions":[{"@odata.id":"/redfish/v1/Chassis/1/PCIeDevices/NIC1/PCIeFunctions/0"}]}`))
			case "/redfish/v1/Chassis/1/PCIeDevices/NIC1/PCIeFunctions/0":
				_, _ = w.Write([]byte(`{"DeviceClass":"NetworkController"}`))
			// The service-wide chassis collection belongs to another system and
			// must not be traversed for this host. A GPU lives in chassis 2.
			case "/redfish/v1/Chassis":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Chassis/2"}]}`))
			case "/redfish/v1/Chassis/2":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Id":"2","PCIeDevices":[{"@odata.id":"/redfish/v1/Chassis/2/PCIeDevices/GPU2"}]}`))
			case "/redfish/v1/Chassis/2/PCIeDevices/GPU2":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Id":"GPU2","Manufacturer":"NVIDIA","Model":"H100","SerialNumber":"GPU-SN-2","PCIeFunctions":[{"@odata.id":"/redfish/v1/Chassis/2/PCIeDevices/GPU2/PCIeFunctions/0"}],"Status":{"State":"Enabled","Health":"OK"}}`))
			case "/redfish/v1/Chassis/2/PCIeDevices/GPU2/PCIeFunctions/0":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"DeviceClass":"ProcessingAccelerators"}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(serviceWideChassisTraversed).To(BeFalse())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(BeEmpty())
	})

	It("does not fall back to the service-wide chassis on a multi-system service", func() {
		serviceWideChassisTraversed := false
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"},"Chassis":{"@odata.id":"/redfish/v1/Chassis"}}`))
			case "/redfish/v1/Systems/1":
				// No Links.Chassis and no system-level GPU views.
				_, _ = w.Write([]byte(`{"Id":"1"}`))
			case "/redfish/v1/Systems":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1"},{"@odata.id":"/redfish/v1/Systems/2"}],"Members@odata.count":2}`))
			// A GPU lives in the service-wide chassis but belongs to another system
			// and must not be attributed to this host.
			case "/redfish/v1/Chassis":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Chassis/2"}]}`))
			case "/redfish/v1/Chassis/2":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Id":"2","PCIeDevices":[{"@odata.id":"/redfish/v1/Chassis/2/PCIeDevices/GPU2"}]}`))
			case "/redfish/v1/Chassis/2/PCIeDevices/GPU2":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Id":"GPU2","Model":"H100","SerialNumber":"GPU-SN-2","PCIeFunctions":[{"@odata.id":"/redfish/v1/Chassis/2/PCIeDevices/GPU2/PCIeFunctions/0"}],"Status":{"State":"Enabled","Health":"OK"}}`))
			case "/redfish/v1/Chassis/2/PCIeDevices/GPU2/PCIeFunctions/0":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"DeviceClass":"ProcessingAccelerators"}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(serviceWideChassisTraversed).To(BeFalse())
		Expect(inventory.Devices).To(BeEmpty())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusUnsupported))
	})

	It("falls back to the service-wide chassis on a single-system service", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"},"Chassis":{"@odata.id":"/redfish/v1/Chassis"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Id":"1"}`))
			case "/redfish/v1/Systems":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1"}],"Members@odata.count":1}`))
			case "/redfish/v1/Chassis":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Chassis/1"}]}`))
			case "/redfish/v1/Chassis/1":
				_, _ = w.Write([]byte(`{"Id":"1","PCIeDevices":[{"@odata.id":"/redfish/v1/Chassis/1/PCIeDevices/GPU1"}]}`))
			case "/redfish/v1/Chassis/1/PCIeDevices/GPU1":
				_, _ = w.Write([]byte(`{"Id":"GPU1","Manufacturer":"NVIDIA","Model":"H100","SerialNumber":"GPU-SN-1","PCIeFunctions":[{"@odata.id":"/redfish/v1/Chassis/1/PCIeDevices/GPU1/PCIeFunctions/0"}],"Status":{"State":"Enabled","Health":"OK"}}`))
			case "/redfish/v1/Chassis/1/PCIeDevices/GPU1/PCIeFunctions/0":
				_, _ = w.Write([]byte(`{"DeviceClass":"ProcessingAccelerators"}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0].Model).To(Equal("H100"))
	})

	It("does not fall back to the service-wide chassis when the Systems collection is paginated", func() {
		serviceWideChassisTraversed := false
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"},"Chassis":{"@odata.id":"/redfish/v1/Chassis"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Id":"1"}`))
			case "/redfish/v1/Systems":
				// One member on this page, but a next link proves more systems exist
				// and there is no authoritative Members@odata.count.
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1"}],"Members@odata.nextLink":"/redfish/v1/Systems?$skip=1"}`))
			case "/redfish/v1/Chassis":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Members":[]}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(serviceWideChassisTraversed).To(BeFalse())
		Expect(inventory.Devices).To(BeEmpty())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusUnsupported))
	})

	It("does not fall back to the service-wide chassis when the Systems listing fails", func() {
		serviceWideChassisTraversed := false
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"},"Chassis":{"@odata.id":"/redfish/v1/Chassis"}}`))
			case "/redfish/v1/Systems":
				// The collection enumeration fails, so the full system set is unknown.
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			case "/redfish/v1/Chassis":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Chassis/1"}]}`))
			case "/redfish/v1/Chassis/1":
				serviceWideChassisTraversed = true
				_, _ = w.Write([]byte(`{"Id":"1","PCIeDevices":[{"@odata.id":"/redfish/v1/Chassis/1/PCIeDevices/GPU1"}]}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		// No system path in the address, so the collector enumerates the Systems
		// collection rather than targeting a single system directly.
		bmh.Spec.BMC.Address = "redfish+http://bmc.example"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(serviceWideChassisTraversed).To(BeFalse())
		Expect(inventory.Devices).To(BeEmpty())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusPartial))
	})

	It("deduplicates a GPU exposed through both Processor and PCIe views, preferring Processor details", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"},"Chassis":{"@odata.id":"/redfish/v1/Chassis"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Processors":{"@odata.id":"/redfish/v1/Systems/1/Processors"},"PCIeDevices":{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices"}}`))
			case "/redfish/v1/Systems/1/Processors":
				if r.URL.Query().Has("$filter") || r.URL.Query().Has("$expand") {
					http.Error(w, "query options are not supported", http.StatusNotImplemented)
					return
				}
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/Processors/CPU0"},{"@odata.id":"/redfish/v1/Systems/1/Processors/GPU_PCIE_A"}]}`))
			case "/redfish/v1/Systems/1/Processors/CPU0":
				_, _ = w.Write([]byte(`{"Id":"CPU0","ProcessorType":"CPU","Model":"CPU model"}`))
			case "/redfish/v1/Systems/1/Processors/GPU_PCIE_A":
				_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/Systems/1/Processors/GPU_PCIE_A","Id":"GPU_PCIE_A","Manufacturer":"Nvidia","Model":"RTXPRO4500BlackwellDC","PartNumber":"2C3A-895-A1","ProcessorType":"GPU","SerialNumber":"1790326076967","Status":{"Health":"OK","State":"Enabled"},"Version":"98.03.B1.00.01"}`))
			// The same physical GPU is also advertised as a PCIe device with the
			// same serial but sparser details. It must be deduplicated against the
			// Processor record even though that record was found first.
			case "/redfish/v1/Systems/1/PCIeDevices":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1"}]}`))
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1":
				_, _ = w.Write([]byte(`{"Id":"GPU1","Manufacturer":"Nvidia","Model":"RTX PRO 4500","SerialNumber":"1790326076967","PCIeFunctions":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions/0"}],"Status":{"State":"Enabled","Health":"OK"}}`))
			case "/redfish/v1/Systems/1/PCIeDevices/GPU1/PCIeFunctions/0":
				_, _ = w.Write([]byte(`{"DeviceClass":"ProcessingAccelerators"}`))
			case "/redfish/v1/Chassis":
				_, _ = w.Write([]byte(`{"Members":[]}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0]).To(Equal(hwmgrcontroller.GPUInfo{
			DeviceClass:     "GPU",
			FirmwareVersion: "98.03.B1.00.01",
			Health:          "OK",
			ID:              "GPU_PCIE_A",
			Manufacturer:    "Nvidia",
			Model:           "RTXPRO4500BlackwellDC",
			PartNumber:      "2C3A-895-A1",
			SerialNumber:    "1790326076967",
			State:           "Enabled",
		}))
	})

	It("merges GPUs exposed through different views on the same system", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Processors":{"@odata.id":"/redfish/v1/Systems/1/Processors"},"GraphicsControllers":{"@odata.id":"/redfish/v1/Systems/1/GraphicsControllers"}}`))
			case "/redfish/v1/Systems/1/Processors":
				if r.URL.Query().Has("$filter") || r.URL.Query().Has("$expand") {
					http.Error(w, "query options are not supported", http.StatusNotImplemented)
					return
				}
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/Processors/GPU_A"}]}`))
			case "/redfish/v1/Systems/1/Processors/GPU_A":
				_, _ = w.Write([]byte(`{"Id":"GPU_A","ProcessorType":"GPU","Manufacturer":"NVIDIA","Model":"H100","SerialNumber":"GPU-SN-A","Status":{"State":"Enabled","Health":"OK"}}`))
			case "/redfish/v1/Systems/1/GraphicsControllers":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/GraphicsControllers/GPU_B"}]}`))
			case "/redfish/v1/Systems/1/GraphicsControllers/GPU_B":
				_, _ = w.Write([]byte(`{"Id":"GPU_B","Manufacturer":"NVIDIA","Model":"L40S","SerialNumber":"GPU-SN-B","Status":{"State":"Enabled","Health":"OK"}}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(HaveLen(2))
		models := []string{inventory.Devices[0].Model, inventory.Devices[1].Model}
		Expect(models).To(ConsistOf("H100", "L40S"))
	})

	It("uses filtered and expanded processor members when the BMC supports them", func() {
		processorCollectionRequests := 0
		queryOptionsMatched := false
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Processors":{"@odata.id":"/redfish/v1/Systems/1/Processors"}}`))
			case "/redfish/v1/Systems/1/Processors":
				processorCollectionRequests++
				queryOptionsMatched = r.URL.Query().Get("$filter") == "ProcessorType eq 'GPU'" && r.URL.Query().Get("$expand") == "."
				_, _ = w.Write([]byte(`{"Members":[{"@odata.type":"#Processor.v1_20_1.Processor","@odata.id":"/redfish/v1/Systems/1/Processors/GPU_0","Id":"GPU_0","Name":"Processor","ProcessorType":"GPU","Manufacturer":"NVIDIA","Model":"GH200 480GB","PartNumber":"2342-888-A1","SerialNumber":"1654523006485","Version":"A1","FirmwareVersion":"96.00.8D.00.03","Status":{"State":"Enabled","Health":"OK"}}]}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0].Model).To(Equal("GH200 480GB"))
		Expect(inventory.Devices[0].FirmwareVersion).To(Equal("96.00.8D.00.03"))
		Expect(processorCollectionRequests).To(Equal(1))
		Expect(queryOptionsMatched).To(BeTrue())
	})

	It("follows collection pagination links when reading GPU processors", func() {
		processorCollectionRequests := 0
		queryOptionsMatched := false
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
			case "/redfish/v1/Systems":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1"}]}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Processors":{"@odata.id":"/redfish/v1/Systems/1/Processors"}}`))
			case "/redfish/v1/Systems/1/Processors":
				processorCollectionRequests++
				if r.URL.Query().Get("$skip") == "1" {
					_, _ = w.Write([]byte(`{"Members":[{"@odata.type":"#Processor.v1_20_1.Processor","@odata.id":"/redfish/v1/Systems/1/Processors/GPU_1","Id":"GPU_1","ProcessorType":"GPU","Manufacturer":"NVIDIA","Model":"H200"}]}`))
					return
				}
				queryOptionsMatched = r.URL.Query().Get("$filter") == "ProcessorType eq 'GPU'" && r.URL.Query().Get("$expand") == "."
				_, _ = w.Write([]byte(`{"Members":[{"@odata.type":"#Processor.v1_20_1.Processor","@odata.id":"/redfish/v1/Systems/1/Processors/GPU_0","Id":"GPU_0","ProcessorType":"GPU","Manufacturer":"NVIDIA","Model":"H100"}],"Members@odata.nextLink":"/redfish/v1/Systems/1/Processors?$filter=ProcessorType%20eq%20%27GPU%27&$expand=.&$skip=1"}`))
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(HaveLen(2))
		Expect(inventory.Devices[0].Model).To(Equal("H100"))
		Expect(inventory.Devices[1].Model).To(Equal("H200"))
		Expect(processorCollectionRequests).To(Equal(2))
		Expect(queryOptionsMatched).To(BeTrue())
	})

	It("keeps reading healthy members after an earlier member fails", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/":
				_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
			case "/redfish/v1/Systems/1":
				_, _ = w.Write([]byte(`{"Processors":{"@odata.id":"/redfish/v1/Systems/1/Processors"}}`))
			case "/redfish/v1/Systems/1/Processors":
				// The first member is a bare link that is unavailable; the second is
				// a healthy expanded GPU. The missing member must not hide it.
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/Processors/GPU_MISSING"},{"@odata.type":"#Processor.v1_20_1.Processor","@odata.id":"/redfish/v1/Systems/1/Processors/GPU_1","Id":"GPU_1","ProcessorType":"GPU","Manufacturer":"NVIDIA","Model":"H100","SerialNumber":"GPU-SN-1","Status":{"State":"Enabled","Health":"OK"}}]}`))
			case "/redfish/v1/Systems/1/Processors/GPU_MISSING":
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusPartial))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0].Model).To(Equal("H100"))
	})

	It("reports a successful empty inventory when no GPU-class functions are present", func() {
		handler = newEmptyGPUInventoryHandler()
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example/redfish/v1/Systems/1"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(inventory.Devices).To(BeEmpty())
	})

	It("marks GPU inventory unsupported when the BMC exposes no relevant Redfish links", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"RedfishVersion":"1.15.0"}`))
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example"

		inventory, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).ToNot(HaveOccurred())
		Expect(inventory.Status).To(Equal(gpuInventoryStatusUnsupported))
		Expect(inventory.Devices).To(BeEmpty())
	})

	It("returns an error when the BMC cannot be queried", func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		})
		reader.httpClient = redfishTestHTTPClient(handler)
		bmh.Spec.BMC.Address = "redfish+http://bmc.example"

		_, err := reader.GetGPUInventory(ctx, bmh)
		Expect(err).To(HaveOccurred())
	})

	It("publishes the base resource before asynchronously discovering GPUs", func() {
		pool := newTestResourcePool("pool")
		bmh.Spec.BMC.Address = "redfish+https://bmc.example/redfish/v1/Systems/1"
		scheme := newTestScheme()
		hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, newTestHardwareData(bmh.Name), pool).Build()
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU1", Model: "H100"}},
		}}
		ds := &HardwareDataSource{
			cloudID:            uuid.New(),
			hubClient:          hubClient,
			gpuInventoryReader: reader,
		}
		events := make(chan *async.AsyncChangeEvent, 3)
		ds.Init(uuid.New(), 1, events)
		workerCtx, cancelWorkers := context.WithCancel(ctx)
		defer cancelWorkers()
		ds.startGPUInventoryWorkers(workerCtx)

		_, err := ds.HandleAsyncEvent(ctx, bmh, async.Updated)
		Expect(err).ToNot(HaveOccurred())
		<-events // ResourceType event
		resourceEvent := <-events
		resource := resourceEvent.Object.(models.Resource)
		Expect(resource.Extensions[gpuInventoryStatusExtension]).To(Equal(gpuInventoryStatusPending))

		var update gpuInventoryUpdate
		Eventually(func() bool {
			select {
			case updateEvent := <-events:
				update, _ = updateEvent.Object.(gpuInventoryUpdate)
				return update.Inventory != nil
			default:
				return false
			}
		}).Should(BeTrue())
		Expect(update.Inventory).To(Equal(&hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU1", Model: "H100"}},
		}))
		Expect(reader.calls).To(Equal(1))
	})

	It("reports GPU discovery failure asynchronously without delaying the server resource", func() {
		pool := newTestResourcePool("pool")
		bmh.Spec.BMC.Address = "redfish+https://bmc.example/redfish/v1/Systems/1"
		scheme := newTestScheme()
		hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, newTestHardwareData(bmh.Name), pool).Build()
		ds := &HardwareDataSource{
			cloudID:            uuid.New(),
			hubClient:          hubClient,
			gpuInventoryReader: staticGPUInventoryReader{err: errors.New("bmc timed out")},
		}
		events := make(chan *async.AsyncChangeEvent, 2)
		ds.Init(uuid.New(), 1, events)

		_, err := ds.HandleAsyncEvent(ctx, bmh, async.Updated)
		Expect(err).ToNot(HaveOccurred())
		<-events // ResourceType event
		resourceEvent := <-events
		resource := resourceEvent.Object.(models.Resource)
		Expect(resource.Extensions[gpuInventoryStatusExtension]).To(Equal(gpuInventoryStatusPending))
		Expect(resource.Extensions).To(HaveKey(vendorExtension))

		ds.processGPUInventoryJob(ctx, resource.ResourceID)
		update := (<-events).Object.(gpuInventoryUpdate)
		Expect(update.Inventory.Status).To(Equal(gpuInventoryStatusUnavailable))
	})

	It("includes GPU data in resource-pool rebuilds", func() {
		pool := newTestResourcePool("pool")
		bmh.Spec.BMC.Address = "redfish+https://bmc.example/redfish/v1/Systems/1"
		scheme := newTestScheme()
		hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, newTestHardwareData(bmh.Name), pool).Build()
		ds := &HardwareDataSource{
			cloudID:   uuid.New(),
			hubClient: hubClient,
			gpuInventoryReader: staticGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
				Status:  gpuInventoryStatusComplete,
				Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU1"}},
			}},
		}
		events := make(chan *async.AsyncChangeEvent, 2)
		ds.Init(uuid.New(), 1, events)

		results, err := ds.BuildResourcesForPool(ctx, "pool")
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].Resource.Extensions[gpuInventoryStatusExtension]).To(Equal(gpuInventoryStatusPending))
		ds.processGPUInventoryJob(ctx, results[0].Resource.ResourceID)
		update := (<-events).Object.(gpuInventoryUpdate)
		Expect(update.Inventory.Devices).To(Equal([]hwmgrcontroller.GPUInfo{{ID: "GPU1"}}))
	})

	It("does not process a queued GPU job after the resource is deleted", func() {
		pool := newTestResourcePool("pool")
		bmh.Spec.BMC.Address = "redfish+https://bmc.example/redfish/v1/Systems/1"
		scheme := newTestScheme()
		hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bmh, newTestHardwareData(bmh.Name), pool).Build()
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{Status: gpuInventoryStatusComplete}}
		ds := &HardwareDataSource{cloudID: uuid.New(), hubClient: hubClient, gpuInventoryReader: reader}
		events := make(chan *async.AsyncChangeEvent, 2)
		ds.Init(uuid.New(), 1, events)

		_, err := ds.HandleAsyncEvent(ctx, bmh, async.Updated)
		Expect(err).ToNot(HaveOccurred())
		<-events // ResourceType event
		resource := (<-events).Object.(models.Resource)
		ds.cancelGPUInventory(resource.ResourceID)
		ds.processGPUInventoryJob(ctx, resource.ResourceID)

		Expect(reader.calls).To(BeZero())
		Expect(events).To(BeEmpty())
	})

	It("re-queries Redfish after a cancelled GPU inventory is cleared from the cache", func() {
		bmh.Spec.BMC.Address = "redfish+https://bmc.example/redfish/v1/Systems/1"
		countingReader := &countingGPUInventoryReader{
			inventory: &hwmgrcontroller.GPUInventory{Status: gpuInventoryStatusComplete},
		}
		ds := &HardwareDataSource{cloudID: uuid.New(), gpuInventoryReader: countingReader}
		resourceID := uuid.MustParse(string(bmh.UID))

		Expect(ds.getGPUInventory(ctx, bmh, true).Status).To(Equal(gpuInventoryStatusComplete))
		Expect(countingReader.calls).To(Equal(1))

		// A second lookup is served from the cache without consulting Redfish.
		Expect(ds.getGPUInventory(ctx, bmh, true).Status).To(Equal(gpuInventoryStatusComplete))
		Expect(countingReader.calls).To(Equal(1))

		// Cancelling the job (as on BMH deletion) evicts the cache entry, so the
		// next lookup queries Redfish again.
		ds.cancelGPUInventory(resourceID)
		Expect(ds.getGPUInventory(ctx, bmh, true).Status).To(Equal(gpuInventoryStatusComplete))
		Expect(countingReader.calls).To(Equal(2))
	})

	It("skips the persisted-inventory read when processing a queued job", func() {
		bmh.Spec.BMC.Address = "redfish+https://bmc.example/redfish/v1/Systems/1"
		reader := &countingGPUInventoryReader{
			inventory: &hwmgrcontroller.GPUInventory{Status: gpuInventoryStatusComplete},
		}
		resourceReader := &staticGPUInventoryResourceReader{}
		ds := &HardwareDataSource{
			cloudID:                    uuid.New(),
			gpuInventoryReader:         reader,
			gpuInventoryResourceReader: resourceReader,
		}
		events := make(chan *async.AsyncChangeEvent, 1)
		ds.Init(uuid.New(), 1, events)
		resourceID := uuid.MustParse(string(bmh.UID))

		ds.enqueueGPUInventory(bmh, resourceID)
		ds.processGPUInventoryJob(ctx, resourceID)

		update := (<-events).Object.(gpuInventoryUpdate)
		Expect(update.Inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(reader.calls).To(Equal(1))
		// prepareGPUInventory already consulted persistence, so the worker must
		// not read the resource again.
		Expect(resourceReader.calls).To(BeZero())
	})

	It("reuses one HTTP client per certificate-verification setting", func() {
		secure1, err := reader.httpClientFor(false)
		Expect(err).ToNot(HaveOccurred())
		secure2, err := reader.httpClientFor(false)
		Expect(err).ToNot(HaveOccurred())
		Expect(secure2).To(BeIdenticalTo(secure1))

		insecure, err := reader.httpClientFor(true)
		Expect(err).ToNot(HaveOccurred())
		Expect(insecure).ToNot(BeIdenticalTo(secure1))

		// An injected override client takes precedence over the cached clients.
		override := redfishTestHTTPClient(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		reader.httpClient = override
		Expect(reader.httpClientFor(false)).To(BeIdenticalTo(override))
	})
})

var _ = Describe("GPU inventory update events", func() {
	It("ignores an update superseded by a newer BMH event", func() {
		dataSourceID := uuid.New()
		resourceID := uuid.New()
		ds := &HardwareDataSource{
			dataSourceID: dataSourceID,
			gpuInventoryJobVersions: map[uuid.UUID]uint64{
				resourceID: 2,
			},
		}
		collector := &Collector{dataSources: []DataSource{ds}}

		err := collector.handleAsyncEvent(context.Background(), &async.AsyncChangeEvent{
			DataSourceID: dataSourceID,
			EventType:    async.Updated,
			Object: gpuInventoryUpdate{
				ResourceID: resourceID,
				Version:    1,
				Inventory:  &hwmgrcontroller.GPUInventory{Status: gpuInventoryStatusComplete},
			},
		})

		Expect(err).ToNot(HaveOccurred())
	})

	It("retains the completed GPU result for persistence retry with capped exponential backoff", func() {
		inventory := &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU1", Model: "H100"}},
		}
		event := &async.AsyncChangeEvent{
			DataSourceID: uuid.New(),
			EventType:    async.Updated,
			Object: gpuInventoryUpdate{
				ResourceID: uuid.New(),
				Version:    7,
				Inventory:  inventory,
			},
		}
		update := event.Object.(gpuInventoryUpdate)
		collector := &Collector{}
		attempt, delay := collector.scheduleGPUInventoryUpdateRetry(event)
		defer collector.gpuInventoryRetryQueue.ShutDown()

		key := gpuInventoryRetryKey{ResourceID: update.ResourceID, Version: update.Version}
		Expect(attempt).To(Equal(1))
		// The delay is the exact value assigned by the rate limiter (first
		// attempt = base delay of one second).
		Expect(delay).To(Equal(time.Second))
		Expect(collector.gpuInventoryRetryQueue.NumRequeues(key)).To(Equal(1))
		Expect(collector.gpuInventoryRetryEvents).To(HaveKey(key))
		retryEvent := collector.gpuInventoryRetryEvents[key]
		Expect(retryEvent).ToNot(BeIdenticalTo(event))
		Expect(retryEvent.DataSourceID).To(Equal(event.DataSourceID))
		Expect(retryEvent.Object).To(Equal(event.Object))

		// A second failure for the same key escalates the delay per the rate
		// limiter's exponential backoff.
		attempt, delay = collector.scheduleGPUInventoryUpdateRetry(event)
		Expect(attempt).To(Equal(2))
		Expect(delay).To(Equal(2 * time.Second))
	})

	It("drops an older pending retry when a newer GPU result fails to persist", func() {
		resourceID := uuid.New()
		dataSourceID := uuid.New()
		collector := &Collector{}

		oldKey := gpuInventoryRetryKey{ResourceID: resourceID, Version: 1}
		newKey := gpuInventoryRetryKey{ResourceID: resourceID, Version: 2}
		collector.scheduleGPUInventoryUpdateRetry(&async.AsyncChangeEvent{
			DataSourceID: dataSourceID,
			EventType:    async.Updated,
			Object:       gpuInventoryUpdate{ResourceID: resourceID, Version: 1, Inventory: &hwmgrcontroller.GPUInventory{}},
		})
		defer collector.gpuInventoryRetryQueue.ShutDown()
		collector.scheduleGPUInventoryUpdateRetry(&async.AsyncChangeEvent{
			DataSourceID: dataSourceID,
			EventType:    async.Updated,
			Object:       gpuInventoryUpdate{ResourceID: resourceID, Version: 2, Inventory: &hwmgrcontroller.GPUInventory{}},
		})

		Expect(collector.gpuInventoryRetryEvents).To(HaveLen(1))
		Expect(collector.gpuInventoryRetryEvents).To(HaveKey(newKey))
		Expect(collector.gpuInventoryRetryEvents).ToNot(HaveKey(oldKey))
		Expect(collector.gpuInventoryRetryQueue.NumRequeues(oldKey)).To(BeZero())
	})
})

func newEmptyGPUInventoryHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/":
			_, _ = w.Write([]byte(`{"Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
		case "/redfish/v1/Systems/1":
			_, _ = w.Write([]byte(`{"Id":"1","GraphicsControllers":{"@odata.id":"/redfish/v1/Systems/1/GraphicsControllers"},"PCIeDevices":{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices"}}`))
		case "/redfish/v1/Systems/1/GraphicsControllers":
			_, _ = w.Write([]byte(`{"Members":[]}`))
		case "/redfish/v1/Systems/1/PCIeDevices":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/Network1"}]}`))
		case "/redfish/v1/Systems/1/PCIeDevices/Network1":
			_, _ = w.Write([]byte(`{"Id":"Network1","Model":"Ethernet Adapter","PCIeFunctions":{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/Network1/PCIeFunctions"}}`))
		case "/redfish/v1/Systems/1/PCIeDevices/Network1/PCIeFunctions":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices/Network1/PCIeFunctions/0"}]}`))
		case "/redfish/v1/Systems/1/PCIeDevices/Network1/PCIeFunctions/0":
			_, _ = w.Write([]byte(`{"DeviceClass":"NetworkController"}`))
		default:
			http.NotFound(w, r)
		}
	})
}

type redfishTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper redfishTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func redfishTestHTTPClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: redfishTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return &http.Response{
			StatusCode: recorder.Code,
			Header:     recorder.Header(),
			Body:       io.NopCloser(bytes.NewReader(recorder.Body.Bytes())),
			Request:    request,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
		}, nil
	})}
}

var _ = Describe("GPU inventory API extension", func() {
	It("does not confuse a missing BMC URL with an empty GPU inventory", func() {
		bmh := newTestBMH("no-bmc", "pool", metal3v1alpha1.StateAvailable)
		ds := &HardwareDataSource{}
		Expect(ds.getGPUInventory(context.Background(), bmh, true)).To(BeNil())
	})

	It("reuses a successful GPU inventory result for later resource rebuilds", func() {
		bmh := newTestBMH("cached-gpu-host", "pool", metal3v1alpha1.StateAvailable)
		bmh.Spec.BMC.Address = "redfish+https://bmc.example"
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU_0", Model: "GH200 480GB"}},
		}}
		ds := &HardwareDataSource{gpuInventoryReader: reader}

		Expect(ds.getGPUInventory(context.Background(), bmh, true).Devices).To(HaveLen(1))
		Expect(ds.getGPUInventory(context.Background(), bmh, true).Devices).To(HaveLen(1))
		Expect(reader.calls).To(Equal(1))
	})

	It("retries partial in-memory inventory on the next resource rebuild", func() {
		bmh := newTestBMH("partial-gpu-host", "pool", metal3v1alpha1.StateAvailable)
		bmh.Spec.BMC.Address = "redfish+https://bmc.example"
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusPartial,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU_0"}},
		}}
		ds := &HardwareDataSource{gpuInventoryReader: reader}

		first := ds.getGPUInventory(context.Background(), bmh, true)
		Expect(first.Status).To(Equal(gpuInventoryStatusPartial))

		reader.inventory = &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU_0", Model: "GH200 480GB"}},
		}
		second := ds.getGPUInventory(context.Background(), bmh, true)

		Expect(second.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(second.Devices[0].Model).To(Equal("GH200 480GB"))
		Expect(reader.calls).To(Equal(2))
	})

	It("reuses the persisted GPU inventory after the datasource is reconstructed", func() {
		bmh := newTestBMH("persisted-gpu-host", "pool", metal3v1alpha1.StateAvailable)
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status: gpuInventoryStatusComplete,
		}}
		resourceReader := &staticGPUInventoryResourceReader{resource: &models.Resource{
			ResourceID: uuid.MustParse(string(bmh.UID)),
			Extensions: map[string]interface{}{
				gpusExtension: []interface{}{map[string]interface{}{
					"id":    "GPU_0",
					"model": "GH200 480GB",
				}},
				gpuInventoryStatusExtension:      gpuInventoryStatusComplete,
				gpuInventoryFingerprintExtension: gpuInventoryFingerprint(bmh),
			},
		}}
		ds := &HardwareDataSource{
			gpuInventoryReader:         reader,
			gpuInventoryResourceReader: resourceReader,
		}

		inventory := ds.getGPUInventory(context.Background(), bmh, true)

		Expect(inventory).To(Equal(&hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU_0", Model: "GH200 480GB"}},
		}))
		Expect(resourceReader.calls).To(Equal(1))
		Expect(reader.calls).To(Equal(0))
	})

	It("re-queries Redfish when the persisted GPU inventory was collected from a different BMC", func() {
		bmh := newTestBMH("repointed-gpu-host", "pool", metal3v1alpha1.StateAvailable)
		bmh.Spec.BMC.Address = "redfish+https://new-bmc.example"
		staleBMH := bmh.DeepCopy()
		staleBMH.Spec.BMC.Address = "redfish+https://old-bmc.example"
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU_0", Model: "GH200 480GB"}},
		}}
		resourceReader := &staticGPUInventoryResourceReader{resource: &models.Resource{
			ResourceID: uuid.MustParse(string(bmh.UID)),
			Extensions: map[string]interface{}{
				gpusExtension:                    []interface{}{map[string]interface{}{"id": "STALE_GPU"}},
				gpuInventoryStatusExtension:      gpuInventoryStatusComplete,
				gpuInventoryFingerprintExtension: gpuInventoryFingerprint(staleBMH),
			},
		}}
		ds := &HardwareDataSource{
			gpuInventoryReader:         reader,
			gpuInventoryResourceReader: resourceReader,
		}

		inventory := ds.getGPUInventory(context.Background(), bmh, true)

		// The persisted record was collected from a different BMC, so it must be
		// re-queried rather than reused.
		Expect(reader.calls).To(Equal(1))
		Expect(inventory.Devices).To(HaveLen(1))
		Expect(inventory.Devices[0].ID).To(Equal("GPU_0"))
	})

	It("re-queries Redfish when the persisted GPU inventory has no BMC fingerprint", func() {
		bmh := newTestBMH("legacy-gpu-host", "pool", metal3v1alpha1.StateAvailable)
		bmh.Spec.BMC.Address = "redfish+https://bmc.example"
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU_0"}},
		}}
		resourceReader := &staticGPUInventoryResourceReader{resource: &models.Resource{
			ResourceID: uuid.MustParse(string(bmh.UID)),
			Extensions: map[string]interface{}{
				gpusExtension:               []interface{}{map[string]interface{}{"id": "STALE_GPU"}},
				gpuInventoryStatusExtension: gpuInventoryStatusComplete,
			},
		}}
		ds := &HardwareDataSource{
			gpuInventoryReader:         reader,
			gpuInventoryResourceReader: resourceReader,
		}

		// A record written before the fingerprint existed is treated as a cache
		// miss and re-queried.
		Expect(ds.getGPUInventory(context.Background(), bmh, true).Devices[0].ID).To(Equal("GPU_0"))
		Expect(reader.calls).To(Equal(1))
	})

	It("keeps the in-memory cache after reusing a persisted GPU inventory", func() {
		bmh := newTestBMH("cache-survives-host", "pool", metal3v1alpha1.StateAvailable)
		bmh.Spec.BMC.Address = "redfish+https://bmc.example"
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status: gpuInventoryStatusComplete,
		}}
		resourceReader := &staticGPUInventoryResourceReader{resource: &models.Resource{
			ResourceID: uuid.MustParse(string(bmh.UID)),
			Extensions: map[string]interface{}{
				gpusExtension: []interface{}{map[string]interface{}{
					"id":    "GPU_0",
					"model": "GH200 480GB",
				}},
				gpuInventoryStatusExtension:      gpuInventoryStatusComplete,
				gpuInventoryFingerprintExtension: gpuInventoryFingerprint(bmh),
			},
		}}
		ds := &HardwareDataSource{
			gpuInventoryReader:         reader,
			gpuInventoryResourceReader: resourceReader,
		}
		ds.Init(uuid.New(), 1, make(chan *async.AsyncChangeEvent, 1))

		for range 2 {
			resource := &models.Resource{
				ResourceID: uuid.MustParse(string(bmh.UID)),
				Extensions: make(map[string]interface{}),
			}
			Expect(ds.prepareGPUInventory(context.Background(), bmh, resource)).To(BeFalse())
			Expect(resource.Extensions[gpusExtension]).To(HaveLen(1))
		}

		// The first call reads persistence and caches the result; the second
		// call must be served from the in-memory cache without a second read.
		Expect(resourceReader.calls).To(Equal(1))
		Expect(reader.calls).To(BeZero())
	})

	It("retries persisted partial inventory instead of treating it as complete", func() {
		bmh := newTestBMH("persisted-partial-gpu-host", "pool", metal3v1alpha1.StateAvailable)
		bmh.Spec.BMC.Address = "redfish+https://bmc.example"
		reader := &countingGPUInventoryReader{inventory: &hwmgrcontroller.GPUInventory{
			Status:  gpuInventoryStatusComplete,
			Devices: []hwmgrcontroller.GPUInfo{{ID: "GPU_0", Model: "GH200 480GB"}},
		}}
		resourceReader := &staticGPUInventoryResourceReader{resource: &models.Resource{
			ResourceID: uuid.MustParse(string(bmh.UID)),
			Extensions: map[string]interface{}{
				gpusExtension:               []interface{}{map[string]interface{}{"id": "GPU_0"}},
				gpuInventoryStatusExtension: gpuInventoryStatusPartial,
			},
		}}
		ds := &HardwareDataSource{
			gpuInventoryReader:         reader,
			gpuInventoryResourceReader: resourceReader,
		}
		events := make(chan *async.AsyncChangeEvent, 1)
		ds.Init(uuid.New(), 1, events)
		resource := &models.Resource{
			ResourceID: uuid.MustParse(string(bmh.UID)),
			Extensions: make(map[string]interface{}),
		}

		shouldQueue := ds.prepareGPUInventory(context.Background(), bmh, resource)

		Expect(shouldQueue).To(BeTrue())
		Expect(resource.Extensions[gpuInventoryStatusExtension]).To(Equal(gpuInventoryStatusPending))
		ds.enqueueGPUInventory(bmh, resource.ResourceID)
		ds.processGPUInventoryJob(context.Background(), resource.ResourceID)
		update := (<-events).Object.(gpuInventoryUpdate)
		Expect(update.Inventory.Status).To(Equal(gpuInventoryStatusComplete))
		Expect(update.Inventory.Devices[0].Model).To(Equal("GH200 480GB"))
		// Only prepareGPUInventory reads the persisted resource; the worker does
		// not read it again.
		Expect(resourceReader.calls).To(Equal(1))
		Expect(reader.calls).To(Equal(1))
	})
})

var _ = Describe("Redfish BMC address parsing", func() {
	It("normalizes Metal3 Redfish driver schemes and extracts the system path", func() {
		root, systemPath, err := parseRedfishBMCAddress("idrac-virtualmedia+https://bmc.example/redfish/v1/Systems/System.Embedded.1")
		Expect(err).ToNot(HaveOccurred())
		Expect(root.String()).To(Equal("https://bmc.example/redfish/v1/"))
		Expect(systemPath).To(Equal("/redfish/v1/Systems/System.Embedded.1"))
	})
})

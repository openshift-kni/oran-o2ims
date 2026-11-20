/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hwmgrcontroller "github.com/openshift-kni/oran-o2ims/internal/hardwaremanager/controller"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	commondb "github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

const (
	// redfishInventoryTimeout bounds a single host's full GPU discovery, which
	// walks the PCIe device and function tree one resource at a time. GET
	// responses are memoized per collection (see redfishSession.getCache), so a
	// host's resources are each fetched once; this budget is headroom for hosts
	// with large PCIe trees, not a crutch for re-reading the same resources.
	redfishInventoryTimeout = 60 * time.Second
	redfishRequestTimeout   = 5 * time.Second
	redfishMaxResponseBytes = 4 << 20
	// redfishWorkerCount is the number of hosts whose GPU inventory can be
	// collected concurrently. redfishMaxConcurrent caps the in-flight Redfish
	// HTTP requests; keep it at least as large as redfishWorkerCount so the
	// request semaphore does not throttle the workers.
	redfishMaxConcurrent = 16
	redfishWorkerCount   = 16

	gpuInventoryStatusComplete    = "complete"
	gpuInventoryStatusPartial     = "partial"
	gpuInventoryStatusUnavailable = "unavailable"
	gpuInventoryStatusUnsupported = "unsupported"
)

var errRedfishNotFound = errors.New("redfish resource not found")

// errRedfishPartial signals that a collection was read only partially: some
// members or pages could not be retrieved, but the returned objects are the
// ones that were read successfully. Callers should mark the inventory partial
// and keep the returned objects rather than discarding them.
var errRedfishPartial = errors.New("redfish collection partially read")

type redfishHTTPStatusError struct {
	statusCode int
}

func (e *redfishHTTPStatusError) Error() string {
	return fmt.Sprintf("redfish GET returned HTTP status %d", e.statusCode)
}

type redfishGPUInventoryReader interface {
	GetGPUInventory(context.Context, *metal3v1alpha1.BareMetalHost) (*hwmgrcontroller.GPUInventory, error)
}

type gpuInventoryResourceReader interface {
	GetResource(context.Context, uuid.UUID) (*models.Resource, error)
}

type gpuInventoryResourceReaderSetter interface {
	setGPUInventoryResourceReader(gpuInventoryResourceReader)
}

type gpuInventoryQueryKey struct {
	resourceKey     string
	bmcAddress      string
	credentialsName string
}

type gpuInventoryCacheEntry struct {
	key       gpuInventoryQueryKey
	inventory *hwmgrcontroller.GPUInventory
}

type gpuInventoryWorkItem struct {
	bmh     *metal3v1alpha1.BareMetalHost
	version uint64
}

type gpuInventoryUpdate struct {
	ResourceID     uuid.UUID
	Version        uint64
	Inventory      *hwmgrcontroller.GPUInventory
	BMCFingerprint string
}

type gpuInventoryRetryKey struct {
	ResourceID uuid.UUID
	Version    uint64
}

type gpuInventoryPersistenceError struct {
	err error
}

func (e *gpuInventoryPersistenceError) Error() string { return e.err.Error() }
func (e *gpuInventoryPersistenceError) Unwrap() error { return e.err }

var _ commondb.Model = gpuInventoryUpdate{}

func (gpuInventoryUpdate) TableName() string  { return "gpu_inventory_update" }
func (gpuInventoryUpdate) PrimaryKey() string { return "resource_id" }
func (gpuInventoryUpdate) OnConflict() string { return "" }

type gpuInventoryUpdateValidator interface {
	isCurrentGPUInventoryUpdate(gpuInventoryUpdate) bool
	completeGPUInventoryUpdate(gpuInventoryUpdate)
}

type redfishGPUInventoryClient struct {
	hubClient client.Reader
	requests  chan struct{}
	// httpClient, when set, overrides the lazily-built clients below. Tests use
	// it to inject a client pointed at an httptest server.
	httpClient *http.Client
	// clientMu guards clientsByInsecure. The clients are built once and reused
	// across inventory collections so TCP/TLS connections to each BMC are
	// pooled rather than re-established per host. The map is keyed by the
	// host's DisableCertificateVerification setting.
	clientMu          sync.Mutex
	clientsByInsecure map[bool]*http.Client
}

func (d *HardwareDataSource) setGPUInventoryResourceReader(reader gpuInventoryResourceReader) {
	d.gpuInventoryResourceReader = reader
}

func (d *HardwareDataSource) ensureGPUInventoryQueueLocked() {
	if d.gpuInventoryQueue == nil {
		d.gpuInventoryQueue = workqueue.NewTyped[uuid.UUID]()
	}
	if d.gpuInventoryJobs == nil {
		d.gpuInventoryJobs = make(map[uuid.UUID]gpuInventoryWorkItem)
	}
	if d.gpuInventoryJobVersions == nil {
		d.gpuInventoryJobVersions = make(map[uuid.UUID]uint64)
	}
}

func (d *HardwareDataSource) startGPUInventoryWorkers(ctx context.Context) {
	d.gpuInventoryWorkersOnce.Do(func() {
		d.gpuInventoryMu.Lock()
		d.ensureGPUInventoryQueueLocked()
		queue := d.gpuInventoryQueue
		d.gpuInventoryMu.Unlock()

		go func() {
			<-ctx.Done()
			queue.ShutDown()
		}()
		for i := 0; i < redfishWorkerCount; i++ {
			go d.runGPUInventoryWorker(ctx, queue)
		}
	})
}

func (d *HardwareDataSource) runGPUInventoryWorker(ctx context.Context, queue workqueue.TypedInterface[uuid.UUID]) {
	for {
		resourceID, shutdown := queue.Get()
		if shutdown {
			return
		}
		d.processGPUInventoryJob(ctx, resourceID)
		queue.Done(resourceID)
	}
}

func (d *HardwareDataSource) processGPUInventoryJob(ctx context.Context, resourceID uuid.UUID) {
	d.gpuInventoryMu.Lock()
	d.ensureGPUInventoryQueueLocked()
	job, ok := d.gpuInventoryJobs[resourceID]
	d.gpuInventoryMu.Unlock()
	if !ok {
		return
	}

	inventory := d.getGPUInventory(ctx, job.bmh, false)
	if inventory == nil || ctx.Err() != nil {
		return
	}

	d.gpuInventoryMu.Lock()
	currentVersion := d.gpuInventoryJobVersions[resourceID]
	if currentVersion != job.version {
		d.gpuInventoryMu.Unlock()
		return
	}
	delete(d.gpuInventoryJobs, resourceID)
	d.gpuInventoryMu.Unlock()

	update := gpuInventoryUpdate{
		ResourceID:     resourceID,
		Version:        job.version,
		Inventory:      cloneGPUInventory(inventory),
		BMCFingerprint: gpuInventoryFingerprint(job.bmh),
	}
	select {
	case <-ctx.Done():
		return
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.Updated,
		Object:       update,
	}:
	}
}

func (d *HardwareDataSource) enqueueGPUInventory(bmh *metal3v1alpha1.BareMetalHost, resourceID uuid.UUID) {
	item := gpuInventoryWorkItem{
		bmh: bmh.DeepCopy(),
	}
	d.gpuInventoryMu.Lock()
	d.ensureGPUInventoryQueueLocked()
	item.version = d.gpuInventoryJobVersion.Add(1)
	d.gpuInventoryJobVersions[resourceID] = item.version
	d.gpuInventoryJobs[resourceID] = item
	queue := d.gpuInventoryQueue
	d.gpuInventoryMu.Unlock()
	queue.Add(resourceID)
}

func (d *HardwareDataSource) cancelGPUInventory(resourceID uuid.UUID) {
	d.gpuInventoryMu.Lock()
	d.cancelGPUInventoryJobLocked(resourceID)
	// This is a terminal cancel (the resource was deleted or excluded from
	// inventory). Drop the job version so no pending persistence retry survives
	// for a resource that no longer exists, and evict the cache so deleted hosts
	// do not retain inventory indefinitely. The cache is keyed by the BMH UID,
	// which equals the resource ID string for every host that reaches this path.
	// Only the deletion path should do this; the reuse paths in
	// prepareGPUInventory cancel the queued job alone so both the cache entry they
	// just populated and any in-flight persistence retry survive.
	delete(d.gpuInventoryJobVersions, resourceID)
	delete(d.gpuInventoryCache, resourceID.String())
	d.gpuInventoryMu.Unlock()
}

// cancelGPUInventoryJobLocked drops any queued inventory job for the resource
// without touching the cache or the job version. Keeping the version valid lets
// an already-completed update awaiting persistence (or its scheduled retry) still
// be recognized as current, so reusing cached inventory during a database outage
// does not strand the update as stale. The caller must hold d.gpuInventoryMu.
func (d *HardwareDataSource) cancelGPUInventoryJobLocked(resourceID uuid.UUID) {
	d.ensureGPUInventoryQueueLocked()
	delete(d.gpuInventoryJobs, resourceID)
}

func (d *HardwareDataSource) isCurrentGPUInventoryUpdate(update gpuInventoryUpdate) bool {
	d.gpuInventoryMu.Lock()
	defer d.gpuInventoryMu.Unlock()
	return d.gpuInventoryJobVersions[update.ResourceID] == update.Version
}

func (d *HardwareDataSource) completeGPUInventoryUpdate(update gpuInventoryUpdate) {
	d.gpuInventoryMu.Lock()
	defer d.gpuInventoryMu.Unlock()
	if d.gpuInventoryJobVersions[update.ResourceID] == update.Version {
		delete(d.gpuInventoryJobVersions, update.ResourceID)
	}
}

// getGPUInventory resolves the GPU inventory for a host from the in-memory
// cache, the persisted resource (when includePersisted is set), or a live
// Redfish query, in that order. Workers pass includePersisted=false because
// prepareGPUInventory already consulted the persisted resource before queuing
// the job, so re-reading it here would duplicate that database read.
func (d *HardwareDataSource) getGPUInventory(
	ctx context.Context,
	bmh *metal3v1alpha1.BareMetalHost,
	includePersisted bool,
) *hwmgrcontroller.GPUInventory {
	if bmh == nil || (d.gpuInventoryReader == nil && d.gpuInventoryResourceReader == nil) {
		return nil
	}
	resourceKey := string(bmh.UID)
	if resourceKey == "" {
		resourceKey = bmh.Namespace + "/" + bmh.Name
	}
	key := gpuInventoryQueryKey{
		resourceKey:     resourceKey,
		bmcAddress:      bmh.Spec.BMC.Address,
		credentialsName: bmh.Spec.BMC.CredentialsName,
	}

	// getGPUInventory is only invoked by the inventory workers, which the
	// workqueue serializes per resourceID (the cache key is the BMH UID), so
	// concurrent calls for the same key cannot occur and no in-flight
	// deduplication is needed.
	d.gpuInventoryMu.Lock()
	if d.gpuInventoryCache == nil {
		d.gpuInventoryCache = make(map[string]gpuInventoryCacheEntry)
	}
	if entry, ok := d.gpuInventoryCache[resourceKey]; ok && entry.key == key {
		if isReusableGPUInventory(entry.inventory) {
			inventory := cloneGPUInventory(entry.inventory)
			d.gpuInventoryMu.Unlock()
			return inventory
		}
		delete(d.gpuInventoryCache, resourceKey)
	}
	d.gpuInventoryMu.Unlock()

	var inventory *hwmgrcontroller.GPUInventory
	if includePersisted {
		inventory = d.getPersistedGPUInventory(ctx, bmh)
	}
	var err error
	queryable := d.gpuInventoryReader != nil && bmh.Spec.BMC.Address != ""
	if inventory == nil && queryable {
		inventory, err = d.gpuInventoryReader.GetGPUInventory(ctx, bmh)
	}
	if inventory == nil && err == nil && !queryable {
		// No persisted inventory and nothing to query.
		return nil
	}
	if err != nil {
		// "unavailable" means the top-level Redfish read failed (auth, TLS, a
		// mis-parsed BMC address, an HTTP error, or a timeout) — not that the
		// host is powered off, since the BMC answers Redfish on standby power.
		// Log the host and BMC address so the failure is attributable.
		slog.WarnContext(ctx, "failed to collect GPU inventory",
			slog.String("bmhName", bmh.Name),
			slog.String("bmhNamespace", bmh.Namespace),
			slog.String("bmcAddress", bmh.Spec.BMC.Address),
			slog.Any("error", err))
		inventory = &hwmgrcontroller.GPUInventory{Status: gpuInventoryStatusUnavailable}
	} else if inventory == nil {
		inventory = &hwmgrcontroller.GPUInventory{Status: gpuInventoryStatusUnavailable}
	}

	if err == nil && isReusableGPUInventory(inventory) {
		d.gpuInventoryMu.Lock()
		d.gpuInventoryCache[resourceKey] = gpuInventoryCacheEntry{
			key:       key,
			inventory: cloneGPUInventory(inventory),
		}
		d.gpuInventoryMu.Unlock()
	}

	return inventory
}

func (d *HardwareDataSource) getPersistedGPUInventory(
	ctx context.Context,
	bmh *metal3v1alpha1.BareMetalHost,
) *hwmgrcontroller.GPUInventory {
	if d.gpuInventoryResourceReader == nil {
		return nil
	}
	resourceID, err := uuid.Parse(string(bmh.UID))
	if err != nil {
		return nil
	}
	resource, err := d.gpuInventoryResourceReader.GetResource(ctx, resourceID)
	if err != nil {
		if !errors.Is(err, svcutils.ErrNotFound) {
			slog.WarnContext(ctx, "failed to load persisted GPU inventory", slog.Any("error", err))
		}
		return nil
	}
	if resource == nil || resource.Extensions == nil {
		return nil
	}
	status, ok := resource.Extensions[gpuInventoryStatusExtension].(string)
	if !ok || (status != gpuInventoryStatusComplete && status != gpuInventoryStatusUnsupported) {
		return nil
	}
	// Reuse the persisted inventory only when it was collected from the BMC the
	// BMH currently points at. A missing fingerprint (records written before
	// this field existed) or a mismatch (the BMH was repointed) is treated as a
	// cache miss so the inventory is queried again.
	fingerprint, _ := resource.Extensions[gpuInventoryFingerprintExtension].(string)
	if fingerprint == "" || fingerprint != gpuInventoryFingerprint(bmh) {
		return nil
	}
	devicesValue, ok := resource.Extensions[gpusExtension]
	if !ok {
		return nil
	}
	devicesJSON, err := json.Marshal(devicesValue)
	if err != nil {
		slog.WarnContext(ctx, "failed to decode persisted GPU inventory", slog.Any("error", err))
		return nil
	}
	var devices []hwmgrcontroller.GPUInfo
	if err := json.Unmarshal(devicesJSON, &devices); err != nil {
		slog.WarnContext(ctx, "failed to decode persisted GPU inventory", slog.Any("error", err))
		return nil
	}
	if devices == nil {
		devices = []hwmgrcontroller.GPUInfo{}
	}
	return &hwmgrcontroller.GPUInventory{Devices: devices, Status: status}
}

func cloneGPUInventory(inventory *hwmgrcontroller.GPUInventory) *hwmgrcontroller.GPUInventory {
	if inventory == nil {
		return nil
	}
	clone := *inventory
	clone.Devices = append([]hwmgrcontroller.GPUInfo(nil), inventory.Devices...)
	return &clone
}

func isReusableGPUInventory(inventory *hwmgrcontroller.GPUInventory) bool {
	// Partial results remain visible on the resource, but a later inventory
	// rebuild must retry Redfish instead of treating them as final.
	return inventory != nil &&
		(inventory.Status == gpuInventoryStatusComplete || inventory.Status == gpuInventoryStatusUnsupported)
}

func (d *HardwareDataSource) prepareGPUInventory(
	ctx context.Context,
	bmh *metal3v1alpha1.BareMetalHost,
	resource *models.Resource,
) bool {
	if bmh == nil || resource == nil {
		return false
	}

	resourceKey := string(bmh.UID)
	if resourceKey == "" {
		resourceKey = bmh.Namespace + "/" + bmh.Name
	}
	key := gpuInventoryQueryKey{
		resourceKey:     resourceKey,
		bmcAddress:      bmh.Spec.BMC.Address,
		credentialsName: bmh.Spec.BMC.CredentialsName,
	}
	d.gpuInventoryMu.Lock()
	if entry, ok := d.gpuInventoryCache[resourceKey]; ok && entry.key == key {
		if isReusableGPUInventory(entry.inventory) {
			inventory := cloneGPUInventory(entry.inventory)
			d.cancelGPUInventoryJobLocked(resource.ResourceID)
			d.gpuInventoryMu.Unlock()
			setGPUInventory(resource, inventory, gpuInventoryFingerprint(bmh))
			return false
		}
		delete(d.gpuInventoryCache, resourceKey)
	}
	d.gpuInventoryMu.Unlock()

	inventory := d.getPersistedGPUInventory(ctx, bmh)
	if isReusableGPUInventory(inventory) {
		d.gpuInventoryMu.Lock()
		if d.gpuInventoryCache == nil {
			d.gpuInventoryCache = make(map[string]gpuInventoryCacheEntry)
		}
		d.gpuInventoryCache[resourceKey] = gpuInventoryCacheEntry{key: key, inventory: cloneGPUInventory(inventory)}
		d.cancelGPUInventoryJobLocked(resource.ResourceID)
		d.gpuInventoryMu.Unlock()
		setGPUInventory(resource, inventory, gpuInventoryFingerprint(bmh))
		return false
	}
	if d.gpuInventoryReader == nil || bmh.Spec.BMC.Address == "" {
		d.cancelGPUInventory(resource.ResourceID)
		return false
	}
	resource.Extensions[gpuInventoryStatusExtension] = gpuInventoryStatusPending
	return true
}

func setGPUInventory(resource *models.Resource, inventory *hwmgrcontroller.GPUInventory, fingerprint string) {
	if resource.Extensions == nil {
		resource.Extensions = make(map[string]interface{})
	}
	resource.Extensions[gpusExtension] = inventory.Devices
	resource.Extensions[gpuInventoryStatusExtension] = inventory.Status
	resource.Extensions[gpuInventoryFingerprintExtension] = fingerprint
}

// gpuInventoryFingerprint hashes the BMC identity (address and credentials
// name) that a GPU inventory was collected from. It is persisted with the
// inventory so a later rebuild can detect a repointed BMC and re-query rather
// than reuse stale data.
func gpuInventoryFingerprint(bmh *metal3v1alpha1.BareMetalHost) string {
	if bmh == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(bmh.Spec.BMC.Address + "\x00" + bmh.Spec.BMC.CredentialsName))
	return hex.EncodeToString(sum[:])
}

func newRedfishGPUInventoryClient(hubClient client.Reader) *redfishGPUInventoryClient {
	return &redfishGPUInventoryClient{
		hubClient: hubClient,
		requests:  make(chan struct{}, redfishMaxConcurrent),
	}
}

// httpClientFor returns an HTTP client suitable for talking to a BMC with the
// given certificate-verification setting. Clients are built once and cached so
// their underlying transports pool connections across inventory collections.
func (c *redfishGPUInventoryClient) httpClientFor(insecure bool) (*http.Client, error) {
	if c.httpClient != nil {
		return c.httpClient, nil
	}

	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if existing, ok := c.clientsByInsecure[insecure]; ok {
		return existing, nil
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not an *http.Transport")
	}
	transport := defaultTransport.Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if transport.TLSClientConfig != nil {
		tlsConfig = transport.TLSClientConfig.Clone()
		if tlsConfig.MinVersion == 0 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	tlsConfig.InsecureSkipVerify = insecure //nolint:gosec // honor explicit BareMetalHost configuration
	transport.TLSClientConfig = tlsConfig
	httpClient := &http.Client{
		Timeout:   redfishRequestTimeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	if c.clientsByInsecure == nil {
		c.clientsByInsecure = make(map[bool]*http.Client)
	}
	c.clientsByInsecure[insecure] = httpClient
	return httpClient, nil
}

func (c *redfishGPUInventoryClient) GetGPUInventory(
	ctx context.Context,
	bmh *metal3v1alpha1.BareMetalHost,
) (*hwmgrcontroller.GPUInventory, error) {
	if bmh.Spec.BMC.Address == "" {
		return nil, errors.New("bmc address is empty")
	}
	if bmh.Spec.BMC.CredentialsName == "" {
		return nil, errors.New("bmc credentials name is empty")
	}

	ctx, cancel := context.WithTimeout(ctx, redfishInventoryTimeout)
	defer cancel()

	select {
	case c.requests <- struct{}{}:
		defer func() { <-c.requests }()
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for a Redfish request slot: %w", ctx.Err())
	}

	var secret corev1.Secret
	if err := c.hubClient.Get(ctx, types.NamespacedName{
		Namespace: bmh.Namespace,
		Name:      bmh.Spec.BMC.CredentialsName,
	}, &secret); err != nil {
		return nil, fmt.Errorf("failed to read BMC credentials Secret: %w", err)
	}
	usernameBytes, usernameOK := secret.Data["username"]
	passwordBytes, passwordOK := secret.Data["password"]
	// Trim surrounding whitespace: BMC credential Secrets are frequently created
	// with a trailing newline (e.g. `echo pass | base64`), which the BMC rejects
	// with HTTP 401. Other Metal3 consumers of the same Secret tolerate this, so
	// trim here too rather than failing GPU collection on a stray newline.
	username := strings.TrimSpace(string(usernameBytes))
	password := strings.TrimSpace(string(passwordBytes))
	if !usernameOK || !passwordOK || username == "" || password == "" {
		return nil, errors.New("bmc credentials secret must contain username and password")
	}

	serviceRoot, systemPath, err := parseRedfishBMCAddress(bmh.Spec.BMC.Address)
	if err != nil {
		return nil, err
	}

	httpClient, err := c.httpClientFor(bmh.Spec.BMC.DisableCertificateVerification)
	if err != nil {
		return nil, err
	}
	session := redfishSession{
		serviceRoot: serviceRoot,
		username:    username,
		password:    password,
		client:      httpClient,
		getCache:    make(map[string][]byte),
	}

	return session.discoverGPUInventory(ctx, systemPath)
}

type redfishSession struct {
	serviceRoot *url.URL
	username    string
	password    string
	client      *http.Client
	// getCache memoizes successful GET response bodies by resolved URL for the
	// duration of one collection. A host exposes the same PCIe devices and
	// functions through several views (system PCIeFunctions, system PCIeDevices,
	// and the Chassis tree), so without this the collector would fetch each
	// resource several times and a large PCIe tree can exhaust the inventory
	// budget. Reads within a single collection are sequential, so no locking is
	// needed.
	getCache map[string][]byte
}

type redfishLink struct {
	ODataID string `json:"@odata.id"`
}

// redfishLinkList decodes a Redfish navigation property that may be either a
// single collection reference ({"@odata.id": ...}) or an array of resource
// links ([{"@odata.id": ...}, ...]). The DMTF ComputerSystem schema defines
// PCIeDevices and PCIeFunctions as arrays of resource links, whereas chassis
// and PCIe device collections use the single-reference form.
type redfishLinkList struct {
	// collection is set when the property is a single collection reference.
	collection redfishLink
	// links is set when the property is an array of resource links.
	links []redfishLink
}

func (l *redfishLinkList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &l.links); err != nil {
			return fmt.Errorf("failed to decode Redfish link array: %w", err)
		}
		return nil
	}
	if err := json.Unmarshal(trimmed, &l.collection); err != nil {
		return fmt.Errorf("failed to decode Redfish collection reference: %w", err)
	}
	return nil
}

func (l redfishLinkList) isEmpty() bool {
	return l.collection.ODataID == "" && len(l.links) == 0
}

type redfishCollection struct {
	Members         []redfishObject `json:"Members"`
	MembersNextLink string          `json:"Members@odata.nextLink"`
	MembersCount    int             `json:"Members@odata.count"`
}

type redfishPartLocation struct {
	ServiceLabel string `json:"ServiceLabel"`
}

type redfishLocation struct {
	PartLocation redfishPartLocation `json:"PartLocation"`
}

type redfishObject struct {
	ODataID             string          `json:"@odata.id"`
	ODataType           string          `json:"@odata.type"`
	ID                  string          `json:"Id"`
	Name                string          `json:"Name"`
	Manufacturer        string          `json:"Manufacturer"`
	Model               string          `json:"Model"`
	SerialNumber        string          `json:"SerialNumber"`
	PartNumber          string          `json:"PartNumber"`
	SKU                 string          `json:"SKU"`
	FirmwareVersion     string          `json:"FirmwareVersion"`
	BiosVersion         string          `json:"BiosVersion"`
	DeviceClass         string          `json:"DeviceClass"`
	BusNumber           string          `json:"BusNumber"`
	DeviceNumber        string          `json:"DeviceNumber"`
	FunctionNumber      string          `json:"FunctionNumber"`
	SegmentNumber       string          `json:"SegmentNumber"`
	FunctionID          *int            `json:"FunctionId"`
	ProcessorType       string          `json:"ProcessorType"`
	Version             string          `json:"Version"`
	Systems             redfishLink     `json:"Systems"`
	Processors          redfishLink     `json:"Processors"`
	GraphicsControllers redfishLink     `json:"GraphicsControllers"`
	PCIeDevices         redfishLinkList `json:"PCIeDevices"`
	PCIeFunctions       redfishLinkList `json:"PCIeFunctions"`
	Chassis             redfishLink     `json:"Chassis"`
	Links               struct {
		PCIeDevice      redfishLink     `json:"PCIeDevice"`
		Chassis         redfishLinkList `json:"Chassis"`
		ComputerSystems redfishLinkList `json:"ComputerSystems"`
	} `json:"Links"`
	Status struct {
		State  string `json:"State"`
		Health string `json:"Health"`
	} `json:"Status"`
	Location redfishLocation `json:"Location"`
	Slot     struct {
		Location redfishLocation `json:"Location"`
	} `json:"Slot"`
}

// isRedfishDriverScheme reports whether a BMC address scheme (with any
// +http/+https transport suffix already stripped) names a Metal3 Redfish driver.
// These all speak the Redfish REST API at /redfish/v1 and default to HTTPS when
// no explicit transport is given.
func isRedfishDriverScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "redfish", "redfish-virtualmedia", "idrac-redfish", "idrac-virtualmedia":
		return true
	default:
		return false
	}
}

func parseRedfishBMCAddress(address string) (*url.URL, string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, "", fmt.Errorf("invalid BMC address: %w", err)
	}
	if parsed.User != nil || parsed.Host == "" {
		return nil, "", fmt.Errorf("bmc address must contain a host and no embedded credentials")
	}

	scheme := parsed.Scheme
	if plus := strings.LastIndex(scheme, "+"); plus >= 0 {
		scheme = scheme[plus+1:]
	} else if isRedfishDriverScheme(scheme) {
		// Metal3 documents the Redfish driver address forms (including the iDRAC
		// and virtual-media variants) as defaulting to HTTPS when no explicit
		// +http/+https transport is given.
		scheme = "https"
	}
	if scheme != "http" && scheme != "https" {
		return nil, "", fmt.Errorf("unsupported BMC address scheme %q", parsed.Scheme)
	}
	parsed.Scheme = scheme
	parsed.RawQuery = ""
	parsed.Fragment = ""

	const serviceRootPath = "/redfish/v1"
	rootIndex := strings.Index(parsed.Path, serviceRootPath)
	rootPath := serviceRootPath
	var systemPath string
	if rootIndex >= 0 {
		rootPath = parsed.Path[:rootIndex+len(serviceRootPath)]
		remainder := strings.Trim(strings.TrimPrefix(parsed.Path[rootIndex+len(serviceRootPath):], "/"), "/")
		segments := strings.Split(remainder, "/")
		if len(segments) == 2 && strings.EqualFold(segments[0], "Systems") {
			systemPath = path.Join(rootPath, segments[0], segments[1])
		}
	} else if parsed.Path != "" && parsed.Path != "/" {
		return nil, "", fmt.Errorf("bmc address path must include %s", serviceRootPath)
	}

	root := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: strings.TrimSuffix(rootPath, "/") + "/"}
	return root, systemPath, nil
}

func (s *redfishSession) discoverGPUInventory(ctx context.Context, directSystemPath string) (*hwmgrcontroller.GPUInventory, error) {
	systems := make([]redfishObject, 0, 1)
	partial := false
	var root redfishObject
	if err := s.get(ctx, s.serviceRoot.String(), &root); err != nil {
		return nil, fmt.Errorf("failed to read Redfish service root: %w", err)
	}

	// systemsComplete is true only when systems holds the service's entire system
	// set. It stays false when a single system was targeted directly (the rest of
	// the service was not enumerated) or when listing the collection failed.
	systemsComplete := false
	if directSystemPath != "" {
		var system redfishObject
		if err := s.get(ctx, directSystemPath, &system); err != nil {
			return nil, fmt.Errorf("failed to read Redfish system: %w", err)
		}
		systems = append(systems, system)
	} else if root.Systems.ODataID != "" {
		var err error
		systems, err = s.list(ctx, root.Systems.ODataID)
		if err != nil {
			s.logPartialRead(ctx, root.Systems.ODataID, err)
			partial = true
		} else {
			systemsComplete = true
		}
	}

	inventory := &hwmgrcontroller.GPUInventory{Devices: make([]hwmgrcontroller.GPUInfo, 0)}
	seen := make(map[string]struct{})
	// Collect every discovery view rather than stopping after one. A GPU present
	// under Processors does not guarantee that every GPU is represented there, so
	// a system can expose distinct GPUs through Processors, GraphicsControllers,
	// PCIe, and chassis. addGPU deduplicates matching physical devices by serial
	// (falling back to ID), and the Processor view is read first so its richer
	// model/firmware details win for devices seen in more than one view.
	endpointFound, processorPartial := s.discoverProcessorGPUs(ctx, systems, inventory, seen)
	partial = partial || processorPartial
	for _, system := range systems {
		devices, found, systemPartial := s.discoverSystemGPUs(ctx, system)
		endpointFound = endpointFound || found
		partial = partial || systemPartial
		for _, device := range devices {
			addGPU(inventory, seen, device)
		}
	}

	// Prefer the chassis associated with the systems we are inventorying so a
	// multi-system Redfish service does not attribute another host's GPUs to
	// this one. Fall back to the service-wide chassis collection only when no
	// system exposes a chassis association (common on single-system BMCs) and the
	// service manages exactly one system, so that collection unambiguously
	// belongs to this host.
	//
	// serviceHasSingleSystem can cost an extra request, so evaluate it lazily and
	// at most once: it is only needed to decide the service-wide fallback or to
	// attribute a linked chassis that omits its ComputerSystems back-reference. A
	// chassis that lists a matching back-reference never consults it.
	singleSystemEvaluated := false
	singleSystemResult := false
	singleSystem := func() bool {
		if !singleSystemEvaluated {
			singleSystemResult = s.serviceHasSingleSystem(ctx, root, systems, systemsComplete, directSystemPath != "")
			singleSystemEvaluated = true
		}
		return singleSystemResult
	}
	chassisLinks := chassisLinksForSystems(systems)
	if chassisLinks.isEmpty() && singleSystem() {
		chassisLinks = redfishLinkList{collection: root.Chassis}
	}
	chassisDevices, chassisFound, chassisPartial := s.discoverChassisGPUs(ctx, chassisLinks, selectedSystemIDs(systems, directSystemPath), singleSystem)
	endpointFound = endpointFound || chassisFound
	partial = partial || chassisPartial
	for _, device := range chassisDevices {
		addGPU(inventory, seen, device)
	}

	switch {
	case partial:
		inventory.Status = gpuInventoryStatusPartial
	case endpointFound:
		inventory.Status = gpuInventoryStatusComplete
	default:
		inventory.Status = gpuInventoryStatusUnsupported
	}
	sort.Slice(inventory.Devices, func(i, j int) bool {
		return inventory.Devices[i].ID < inventory.Devices[j].ID
	})
	return inventory, nil
}

// serviceHasSingleSystem reports whether the Redfish service manages exactly one
// system, which is the only case where the service-wide chassis collection can
// be attributed to this host without risking another system's GPUs. Any
// uncertainty returns false so the fallback is skipped.
//
// When the whole Systems collection was enumerated (directSystem is false), the
// result is trusted only if that listing succeeded (systemsComplete); a failed
// or partial listing must not enable the fallback. When a single system was
// targeted directly, the collection was never enumerated, so the Systems
// collection count is consulted and must be authoritative: an explicit
// Members@odata.count, or a single member with no further pages.
func (s *redfishSession) serviceHasSingleSystem(ctx context.Context, root redfishObject, systems []redfishObject, systemsComplete, directSystem bool) bool {
	if !directSystem {
		return systemsComplete && len(systems) == 1
	}
	if root.Systems.ODataID == "" {
		return false
	}
	var collection redfishCollection
	if err := s.get(ctx, root.Systems.ODataID, &collection); err != nil {
		return false
	}
	if collection.MembersCount > 0 {
		return collection.MembersCount == 1
	}
	// Without an authoritative count, a single member only proves a single-system
	// service when the collection is not paginated.
	if collection.MembersNextLink != "" {
		return false
	}
	return len(collection.Members) == 1
}

// logPartialRead records the specific Redfish read that forced a partial GPU
// inventory, so the cause is visible in logs instead of only the aggregate
// "partial" status.
func (s *redfishSession) logPartialRead(ctx context.Context, link string, err error) {
	slog.WarnContext(ctx, "GPU inventory incomplete: Redfish read failed",
		slog.String("link", link),
		slog.String("serviceRoot", s.serviceRoot.String()),
		slog.Any("error", err))
}

func (s *redfishSession) discoverProcessorGPUs(
	ctx context.Context,
	systems []redfishObject,
	inventory *hwmgrcontroller.GPUInventory,
	seen map[string]struct{},
) (bool, bool) {
	endpointFound := false
	partial := false
	for _, system := range systems {
		if system.Processors.ODataID == "" {
			continue
		}
		endpointFound = true
		processors, err := s.listProcessors(ctx, system.Processors.ODataID)
		if err != nil && !errors.Is(err, errRedfishNotFound) {
			s.logPartialRead(ctx, system.Processors.ODataID, err)
			partial = true
		}
		for _, processor := range processors {
			if !strings.EqualFold(processor.ProcessorType, "GPU") {
				// An explicit non-GPU Processor classification (e.g. an FPGA) wins
				// over the broad PCIe ProcessingAccelerators class: block this
				// device's identity so a later view cannot re-add it as a GPU.
				if processor.ProcessorType != "" {
					markNonGPU(seen, processor.SerialNumber,
						redfishObjectID(processor), processor.Links.PCIeDevice.ODataID)
				}
				continue
			}
			addGPU(inventory, seen, gpuInfoFromProcessor(processor))
		}
	}
	return endpointFound, partial
}

func (s *redfishSession) discoverSystemGPUs(ctx context.Context, system redfishObject) ([]hwmgrcontroller.GPUInfo, bool, bool) {
	devices := make([]hwmgrcontroller.GPUInfo, 0)
	endpointFound := false
	partial := false

	if system.GraphicsControllers.ODataID != "" {
		endpointFound = true
		graphics, err := s.list(ctx, system.GraphicsControllers.ODataID)
		if err != nil && !errors.Is(err, errRedfishNotFound) {
			s.logPartialRead(ctx, system.GraphicsControllers.ODataID, err)
			partial = true
		}
		for _, graphicsController := range graphics {
			devices = append(devices, gpuInfoFromGraphicsController(graphicsController))
		}
	}

	if !system.PCIeFunctions.isEmpty() {
		endpointFound = true
		functions, funcPartial := s.resolveLinkList(ctx, system.PCIeFunctions)
		partial = partial || funcPartial
		for _, function := range functions {
			if !isGPUDeviceClass(function.DeviceClass) {
				continue
			}
			gpu := gpuInfoFromPCIeFunction(function)
			if function.Links.PCIeDevice.ODataID != "" {
				var device redfishObject
				if err := s.get(ctx, function.Links.PCIeDevice.ODataID, &device); err != nil {
					if !errors.Is(err, errRedfishNotFound) {
						s.logPartialRead(ctx, function.Links.PCIeDevice.ODataID, err)
						partial = true
					}
				} else {
					// Skip an absent parent device, consistently with the
					// PCIeDevices traversal, so inventory does not depend on which
					// navigation property exposes the same device.
					if strings.EqualFold(device.Status.State, "Absent") {
						continue
					}
					gpu = gpuInfoFromPCIeDevice(device, function)
				}
			}
			devices = append(devices, gpu)
		}
	}

	if !system.PCIeDevices.isEmpty() {
		endpointFound = true
		pcieDevices, devicePartial := s.discoverPCIeDevices(ctx, system.PCIeDevices)
		partial = partial || devicePartial
		devices = append(devices, pcieDevices...)
	}
	return devices, endpointFound, partial
}

// chassisLinksForSystems collects the chassis referenced by the given systems'
// Links.Chassis, deduplicated by @odata.id. The result scopes chassis discovery
// to the chassis associated with the systems being inventoried.
func chassisLinksForSystems(systems []redfishObject) redfishLinkList {
	seen := make(map[string]struct{})
	var list redfishLinkList
	appendLink := func(link redfishLink) {
		if link.ODataID == "" {
			return
		}
		if _, ok := seen[link.ODataID]; ok {
			return
		}
		seen[link.ODataID] = struct{}{}
		list.links = append(list.links, link)
	}
	for _, system := range systems {
		appendLink(system.Links.Chassis.collection)
		for _, link := range system.Links.Chassis.links {
			appendLink(link)
		}
	}
	return list
}

func (s *redfishSession) discoverChassisGPUs(ctx context.Context, chassisLinks redfishLinkList, selectedSystems map[string]struct{}, singleSystem func() bool) ([]hwmgrcontroller.GPUInfo, bool, bool) {
	if chassisLinks.isEmpty() {
		return nil, false, false
	}
	chassis, partial := s.resolveLinkList(ctx, chassisLinks)
	devices := make([]hwmgrcontroller.GPUInfo, 0)
	endpointFound := false
	for _, item := range chassis {
		if item.PCIeDevices.isEmpty() {
			continue
		}
		// A chassis can host several ComputerSystems. Its PCIe collection is the
		// union of every hosted system's devices, so attribute it to this host
		// only when ownership is positively established. When it cannot be, the
		// chassis may hold this host's GPUs that we cannot safely attribute, so
		// mark the inventory partial rather than silently reporting it complete.
		if !chassisOwnedBySelectedSystems(item, selectedSystems, singleSystem) {
			partial = true
			continue
		}
		endpointFound = true
		pcieDevices, devicePartial := s.discoverPCIeDevices(ctx, item.PCIeDevices)
		partial = partial || devicePartial
		devices = append(devices, pcieDevices...)
	}
	return devices, endpointFound, partial
}

// selectedSystemIDs returns the set of @odata.ids for the systems being
// inventoried, normalized so chassis back-references can be matched regardless
// of a trailing slash. directSystemPath is included because a directly targeted
// system is identified by its request path even when the response body omits
// @odata.id.
func selectedSystemIDs(systems []redfishObject, directSystemPath string) map[string]struct{} {
	ids := make(map[string]struct{}, len(systems)+1)
	if id := normalizeODataID(directSystemPath); id != "" {
		ids[id] = struct{}{}
	}
	for _, system := range systems {
		if id := normalizeODataID(system.ODataID); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

// chassisOwnedBySelectedSystems reports whether a chassis's devices can be
// attributed to the selected systems. Ownership must be positively established:
//
//   - When the chassis lists ComputerSystems back-references, every one must be in
//     the selected set; any other system makes the chassis shared and ambiguous.
//   - When the chassis advertises no back-references, a missing reverse link does
//     not by itself prove exclusivity, so it is owned only when the service
//     manages a single system (singleSystem), where no other system can claim it.
//
// Any other case is treated as not owned so another host's GPUs are not reported
// for this one. singleSystem is evaluated lazily and only consulted when the
// chassis omits its back-references, so a correctly linked chassis costs no extra
// request.
func chassisOwnedBySelectedSystems(chassis redfishObject, selectedSystems map[string]struct{}, singleSystem func() bool) bool {
	links := chassis.Links.ComputerSystems
	ids := make([]string, 0, len(links.links)+1)
	if id := normalizeODataID(links.collection.ODataID); id != "" {
		ids = append(ids, id)
	}
	for _, link := range links.links {
		if id := normalizeODataID(link.ODataID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return singleSystem()
	}
	for _, id := range ids {
		if _, ok := selectedSystems[id]; !ok {
			return false
		}
	}
	return true
}

func normalizeODataID(id string) string {
	return strings.TrimRight(id, "/")
}

func (s *redfishSession) discoverPCIeDevices(ctx context.Context, list redfishLinkList) ([]hwmgrcontroller.GPUInfo, bool) {
	pcieDevices, partial := s.resolveLinkList(ctx, list)
	devices := make([]hwmgrcontroller.GPUInfo, 0)
	for _, device := range pcieDevices {
		// A device that is Absent, or present but exposes no PCIeFunctions link,
		// contributes no GPU data. A missing functions link is not a partial read
		// (an actual fetch error is flagged by resolveLinkList below); many BMCs
		// list present devices with no functions subtree, so skip them without
		// marking the inventory partial.
		if strings.EqualFold(device.Status.State, "Absent") || device.PCIeFunctions.isEmpty() {
			continue
		}
		functions, funcPartial := s.resolveLinkList(ctx, device.PCIeFunctions)
		partial = partial || funcPartial
		for _, function := range functions {
			if isGPUDeviceClass(function.DeviceClass) {
				devices = append(devices, gpuInfoFromPCIeDevice(device, function))
			}
		}
	}
	return devices, partial
}

// resolveLinkList returns the Redfish objects referenced by a link list. A
// single collection reference is listed; an array of resource links is fetched
// element by element. Individual element failures mark the result partial but do
// not stop the remaining elements.
func (s *redfishSession) resolveLinkList(ctx context.Context, list redfishLinkList) ([]redfishObject, bool) {
	if len(list.links) > 0 {
		objects := make([]redfishObject, 0, len(list.links))
		partial := false
		for _, link := range list.links {
			if link.ODataID == "" {
				continue
			}
			var object redfishObject
			if err := s.get(ctx, link.ODataID, &object); err != nil {
				// A 404 means the referenced device is genuinely absent (the BMC
				// advertised a link it no longer serves); skip it without marking
				// the result partial. Other errors are transient read failures.
				if !errors.Is(err, errRedfishNotFound) {
					s.logPartialRead(ctx, link.ODataID, err)
					partial = true
				}
				continue
			}
			objects = append(objects, object)
		}
		return objects, partial
	}
	if list.collection.ODataID != "" {
		objects, err := s.list(ctx, list.collection.ODataID)
		// An advertised collection that returns 404 is absent, not a partial
		// read (some Dell iDRAC systems advertise a System PCIeDevices link that
		// 404s while the real devices live under Chassis).
		if err != nil && !errors.Is(err, errRedfishNotFound) {
			s.logPartialRead(ctx, list.collection.ODataID, err)
			return objects, true
		}
		return objects, false
	}
	return nil, false
}

func (s *redfishSession) list(ctx context.Context, link string) ([]redfishObject, error) {
	return s.listWithQuery(ctx, link, nil)
}

// listProcessors returns every member of a Processors collection, not only GPUs.
// Non-GPU entries are needed so the caller can honor an explicit non-GPU
// classification (e.g. an FPGA) and keep a broad PCIe ProcessingAccelerators
// device from being counted as a GPU. A server-side ProcessorType filter would
// hide those entries, so only $expand is requested to collapse the per-member
// GETs into one response where supported.
func (s *redfishSession) listProcessors(ctx context.Context, link string) ([]redfishObject, error) {
	query := make(url.Values)
	query.Set("$expand", ".")

	processors, err := s.listWithQuery(ctx, link, query)
	if err == nil {
		return processors, nil
	}
	if errors.Is(err, errRedfishPartial) {
		// The query was accepted; some members were just unreadable. Keep the
		// processors we did read and propagate the partial signal rather than
		// retrying the whole collection without query options.
		return processors, err
	}
	var statusError *redfishHTTPStatusError
	if !errors.As(err, &statusError) ||
		(statusError.statusCode != http.StatusBadRequest && statusError.statusCode != http.StatusNotImplemented) {
		// A non-partial, non-query-rejection failure (e.g. the inventory context
		// expiring mid-traversal) can still carry processors read before it
		// occurred. Keep them so a later interruption cannot drop already-known
		// GPUs; the error propagates so discovery is marked partial.
		return processors, err
	}

	// Query parameters are optional in Redfish. Retry with the plain collection
	// traversal when the BMC does not support expansion.
	return s.list(ctx, link)
}

func (s *redfishSession) listWithQuery(ctx context.Context, link string, query url.Values) ([]redfishObject, error) {
	if len(query) > 0 {
		target, err := url.Parse(link)
		if err != nil {
			return nil, fmt.Errorf("invalid Redfish link: %w", err)
		}
		queryValues := target.Query()
		for key, values := range query {
			queryValues[key] = values
		}
		target.RawQuery = queryValues.Encode()
		link = target.String()
	}

	objects := make([]redfishObject, 0)
	seenPages := make(map[string]struct{})
	partial := false
	firstPage := true
	for nextPage := link; nextPage != ""; {
		if _, seen := seenPages[nextPage]; seen {
			return objects, errors.New("redfish collection repeated a pagination link")
		}
		seenPages[nextPage] = struct{}{}

		var collection redfishCollection
		if err := s.get(ctx, nextPage, &collection); err != nil {
			// The first page failing is a genuine collection error; callers rely on
			// its status (e.g. to retry without query options). A later page failing
			// means we already have earlier pages, so report partial and keep them.
			if firstPage {
				return objects, err
			}
			s.logPartialRead(ctx, nextPage, err)
			partial = true
			break
		}
		firstPage = false
		for _, member := range collection.Members {
			if member.ODataID == "" {
				continue
			}
			if isExpandedRedfishObject(member) {
				objects = append(objects, member)
				continue
			}
			var object redfishObject
			if err := s.get(ctx, member.ODataID, &object); err != nil {
				// One unavailable member must not hide the rest. Record that the
				// result is partial and continue, unless the context is no longer
				// usable, in which case further requests would fail anyway.
				if ctx.Err() != nil {
					return objects, err
				}
				s.logPartialRead(ctx, member.ODataID, err)
				partial = true
				continue
			}
			objects = append(objects, object)
		}
		nextPage = collection.MembersNextLink
	}

	if partial {
		return objects, errRedfishPartial
	}
	return objects, nil
}

func isExpandedRedfishObject(object redfishObject) bool {
	return object.ODataType != "" || object.ID != "" || object.Name != "" ||
		object.ProcessorType != "" || object.Model != "" || object.Manufacturer != ""
}

func (s *redfishSession) get(ctx context.Context, link string, result any) error {
	reference, err := url.Parse(link)
	if err != nil {
		return fmt.Errorf("invalid Redfish link: %w", err)
	}
	target := s.serviceRoot.ResolveReference(reference)
	if target.User != nil || !strings.EqualFold(target.Scheme, s.serviceRoot.Scheme) || !strings.EqualFold(target.Host, s.serviceRoot.Host) {
		return fmt.Errorf("redfish link points outside the configured BMC origin")
	}
	cacheKey := target.String()
	if cached, ok := s.getCache[cacheKey]; ok {
		if err := json.Unmarshal(cached, result); err != nil {
			return fmt.Errorf("failed to decode cached Redfish response: %w", err)
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cacheKey, nil)
	if err != nil {
		return fmt.Errorf("failed to create Redfish request: %w", err)
	}
	request.SetBasicAuth(s.username, s.password)
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("redfish GET failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return errRedfishNotFound
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &redfishHTTPStatusError{statusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, redfishMaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("failed to read Redfish response: %w", err)
	}
	if len(body) > redfishMaxResponseBytes {
		return fmt.Errorf("redfish response exceeded %d bytes", redfishMaxResponseBytes)
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("failed to decode Redfish response: %w", err)
	}
	if s.getCache != nil {
		s.getCache[cacheKey] = body
	}
	return nil
}

func isGPUDeviceClass(deviceClass string) bool {
	return strings.EqualFold(deviceClass, "DisplayController") ||
		strings.EqualFold(deviceClass, "ProcessingAccelerators")
}

func gpuInfoFromGraphicsController(object redfishObject) hwmgrcontroller.GPUInfo {
	id := object.Links.PCIeDevice.ODataID
	if id == "" {
		id = redfishObjectID(object)
	}
	firmwareVersion := object.FirmwareVersion
	if firmwareVersion == "" {
		firmwareVersion = object.BiosVersion
	}
	return hwmgrcontroller.GPUInfo{
		DeviceClass:     "GraphicsController",
		FirmwareVersion: firmwareVersion,
		Health:          object.Status.Health,
		ID:              id,
		Location:        redfishLocationLabel(object),
		Manufacturer:    object.Manufacturer,
		Model:           object.Model,
		PartNumber:      object.PartNumber,
		SerialNumber:    object.SerialNumber,
		SKU:             object.SKU,
		State:           object.Status.State,
	}
}

func gpuInfoFromProcessor(processor redfishObject) hwmgrcontroller.GPUInfo {
	firmwareVersion := processor.FirmwareVersion
	if firmwareVersion == "" {
		firmwareVersion = processor.Version
	}
	return hwmgrcontroller.GPUInfo{
		DeviceClass:     "GPU",
		FirmwareVersion: firmwareVersion,
		Health:          processor.Status.Health,
		ID:              redfishObjectID(processor),
		Location:        redfishLocationLabel(processor),
		Manufacturer:    processor.Manufacturer,
		Model:           processor.Model,
		PartNumber:      processor.PartNumber,
		SerialNumber:    processor.SerialNumber,
		SKU:             processor.SKU,
		State:           processor.Status.State,
	}
}

func gpuInfoFromPCIeDevice(device, function redfishObject) hwmgrcontroller.GPUInfo {
	id := device.ODataID
	if id == "" {
		id = redfishObjectID(device)
	}
	return hwmgrcontroller.GPUInfo{
		DeviceClass:     function.DeviceClass,
		FirmwareVersion: device.FirmwareVersion,
		Health:          device.Status.Health,
		ID:              id,
		Location:        redfishLocationLabel(device),
		Manufacturer:    device.Manufacturer,
		Model:           device.Model,
		PartNumber:      device.PartNumber,
		PCIAddress:      redfishPCIAddress(function),
		SerialNumber:    device.SerialNumber,
		SKU:             device.SKU,
		State:           device.Status.State,
	}
}

func gpuInfoFromPCIeFunction(function redfishObject) hwmgrcontroller.GPUInfo {
	return hwmgrcontroller.GPUInfo{
		DeviceClass:     function.DeviceClass,
		FirmwareVersion: function.FirmwareVersion,
		Health:          function.Status.Health,
		ID:              redfishObjectID(function),
		Location:        redfishLocationLabel(function),
		Manufacturer:    function.Manufacturer,
		Model:           function.Model,
		PartNumber:      function.PartNumber,
		PCIAddress:      redfishPCIAddress(function),
		SerialNumber:    function.SerialNumber,
		SKU:             function.SKU,
		State:           function.Status.State,
	}
}

func redfishLocationLabel(object redfishObject) string {
	if object.Location.PartLocation.ServiceLabel != "" {
		return object.Location.PartLocation.ServiceLabel
	}
	return object.Slot.Location.PartLocation.ServiceLabel
}

// redfishObjectID returns a stable identity for a Redfish resource. The
// canonical @odata.id is preferred because it is unique across the whole service
// and encodes the owning ComputerSystem, so GPUs from different systems that
// share a collection-local Id (e.g. "GPU0") do not collide in the per-service
// deduplication map. The local Id is used only when no @odata.id is available.
func redfishObjectID(object redfishObject) string {
	if object.ODataID != "" {
		return object.ODataID
	}
	return object.ID
}

// addGPU appends a GPU unless the same physical device was already recorded. A
// device may surface through several views that each populate a different subset
// of identifiers: one view may report a serial number while another reports only
// the device ID. Matching on either alias prevents the same GPU from being
// counted twice. The first view to record a device wins, so callers order the
// richer views (Processors) first.
func addGPU(inventory *hwmgrcontroller.GPUInventory, seen map[string]struct{}, gpu hwmgrcontroller.GPUInfo) {
	serialKey := gpuSerialKey(gpu.SerialNumber)
	idKey := gpuIDKey(gpu.ID)
	if serialKey == "" && idKey == "" {
		return
	}
	// Determine a match before registering, then register every alias this view
	// supplies even on a match. A device seen first by serial and later by a new
	// ID (or vice versa) must have both aliases recorded, so a third view that
	// shares only the newly learned alias is still deduplicated rather than added
	// as a duplicate.
	matched := false
	if serialKey != "" {
		if _, exists := seen[serialKey]; exists {
			matched = true
		}
	}
	if idKey != "" {
		if _, exists := seen[idKey]; exists {
			matched = true
		}
	}
	if serialKey != "" {
		seen[serialKey] = struct{}{}
	}
	if idKey != "" {
		seen[idKey] = struct{}{}
	}
	if matched {
		return
	}
	inventory.Devices = append(inventory.Devices, gpu)
}

// markNonGPU records a device's identity aliases in seen without adding it to the
// inventory, so a later view cannot (re)classify the same physical device as a
// GPU. It is used for devices the Processor view explicitly classifies as
// non-GPU: the broad PCIe DeviceClass ProcessingAccelerators also covers non-GPU
// accelerators such as FPGAs, and that class alone must not override an explicit
// Processor classification for the same device.
func markNonGPU(seen map[string]struct{}, serial string, ids ...string) {
	if key := gpuSerialKey(serial); key != "" {
		seen[key] = struct{}{}
	}
	for _, id := range ids {
		if key := gpuIDKey(id); key != "" {
			seen[key] = struct{}{}
		}
	}
}

func gpuSerialKey(serial string) string {
	if serial == "" {
		return ""
	}
	return "serial:" + strings.ToLower(serial)
}

func gpuIDKey(id string) string {
	if id == "" {
		return ""
	}
	return "id:" + id
}

func redfishPCIAddress(function redfishObject) string {
	bus := function.BusNumber
	device := function.DeviceNumber
	functionNumber := function.FunctionNumber
	if functionNumber == "" && function.FunctionID != nil {
		functionNumber = strconv.Itoa(*function.FunctionID)
	}
	if bus == "" || device == "" || functionNumber == "" {
		return ""
	}
	segment := function.SegmentNumber
	if segment == "" {
		segment = "0"
	}
	return fmt.Sprintf("%s:%s:%s.%s", pciHex(segment, 4), pciHex(bus, 2), pciHex(device, 2), pciHex(functionNumber, 1))
}

func pciHex(value string, width int) string {
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	number, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%0*x", width, number)
}

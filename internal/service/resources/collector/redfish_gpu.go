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
	redfishInventoryTimeout = 30 * time.Second
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
	// The cache is keyed by the BMH UID, which equals the resource ID string
	// for every host that reaches this path; drop it so deleted hosts do not
	// retain cached inventory indefinitely. Only the deletion path should evict
	// the cache; the reuse paths in prepareGPUInventory cancel the job alone so
	// the entry they just populated survives.
	delete(d.gpuInventoryCache, resourceID.String())
	d.gpuInventoryMu.Unlock()
}

// cancelGPUInventoryJobLocked drops any in-flight inventory job for the resource
// without touching the cache. The caller must hold d.gpuInventoryMu.
func (d *HardwareDataSource) cancelGPUInventoryJobLocked(resourceID uuid.UUID) {
	d.ensureGPUInventoryQueueLocked()
	delete(d.gpuInventoryJobs, resourceID)
	delete(d.gpuInventoryJobVersions, resourceID)
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
		slog.WarnContext(ctx, "failed to collect GPU inventory", slog.Any("error", err))
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
	username, usernameOK := secret.Data["username"]
	password, passwordOK := secret.Data["password"]
	if !usernameOK || !passwordOK || len(username) == 0 || len(password) == 0 {
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
		username:    string(username),
		password:    string(password),
		client:      httpClient,
	}

	return session.discoverGPUInventory(ctx, systemPath)
}

type redfishSession struct {
	serviceRoot *url.URL
	username    string
	password    string
	client      *http.Client
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
		PCIeDevice redfishLink `json:"PCIeDevice"`
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
	} else if scheme == "redfish" {
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
			partial = true
		}
	}

	inventory := &hwmgrcontroller.GPUInventory{Devices: make([]hwmgrcontroller.GPUInfo, 0)}
	seen := make(map[string]struct{})
	processorGPUs, processorGPUFound, endpointFound, processorPartial := s.discoverProcessorGPUs(ctx, systems, inventory, seen)
	partial = partial || processorPartial
	for i, system := range systems {
		// Some BMCs expose the same GPU as both a Processor and a PCIe device.
		// Prefer the Processor record because it commonly has the GPU model and
		// firmware version that the PCIe device omits.
		if processorGPUs[i] {
			continue
		}
		devices, found, systemPartial := s.discoverSystemGPUs(ctx, system)
		endpointFound = endpointFound || found
		partial = partial || systemPartial
		for _, device := range devices {
			addGPU(inventory, seen, device)
		}
	}

	if !processorGPUFound {
		devices, found, chassisPartial := s.discoverChassisGPUs(ctx, root.Chassis.ODataID)
		endpointFound = endpointFound || found
		partial = partial || chassisPartial
		for _, device := range devices {
			addGPU(inventory, seen, device)
		}
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

func (s *redfishSession) discoverProcessorGPUs(
	ctx context.Context,
	systems []redfishObject,
	inventory *hwmgrcontroller.GPUInventory,
	seen map[string]struct{},
) ([]bool, bool, bool, bool) {
	processorGPUs := make([]bool, len(systems))
	processorGPUFound := false
	endpointFound := false
	partial := false
	for i, system := range systems {
		if system.Processors.ODataID == "" {
			continue
		}
		endpointFound = true
		processors, err := s.listProcessorGPUs(ctx, system.Processors.ODataID)
		if err != nil {
			partial = true
		}
		for _, processor := range processors {
			if !strings.EqualFold(processor.ProcessorType, "GPU") {
				continue
			}
			processorGPUs[i] = true
			processorGPUFound = true
			addGPU(inventory, seen, gpuInfoFromProcessor(processor))
		}
	}
	return processorGPUs, processorGPUFound, endpointFound, partial
}

func (s *redfishSession) discoverSystemGPUs(ctx context.Context, system redfishObject) ([]hwmgrcontroller.GPUInfo, bool, bool) {
	devices := make([]hwmgrcontroller.GPUInfo, 0)
	endpointFound := false
	partial := false

	if system.GraphicsControllers.ODataID != "" {
		endpointFound = true
		graphics, err := s.list(ctx, system.GraphicsControllers.ODataID)
		if err != nil {
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
					partial = true
				} else {
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

func (s *redfishSession) discoverChassisGPUs(ctx context.Context, chassisPath string) ([]hwmgrcontroller.GPUInfo, bool, bool) {
	if chassisPath == "" {
		return nil, false, false
	}
	chassis, err := s.list(ctx, chassisPath)
	partial := err != nil
	devices := make([]hwmgrcontroller.GPUInfo, 0)
	endpointFound := false
	for _, item := range chassis {
		if item.PCIeDevices.isEmpty() {
			continue
		}
		endpointFound = true
		pcieDevices, devicePartial := s.discoverPCIeDevices(ctx, item.PCIeDevices)
		partial = partial || devicePartial
		devices = append(devices, pcieDevices...)
	}
	return devices, endpointFound, partial
}

func (s *redfishSession) discoverPCIeDevices(ctx context.Context, list redfishLinkList) ([]hwmgrcontroller.GPUInfo, bool) {
	pcieDevices, partial := s.resolveLinkList(ctx, list)
	devices := make([]hwmgrcontroller.GPUInfo, 0)
	for _, device := range pcieDevices {
		if strings.EqualFold(device.Status.State, "Absent") || device.PCIeFunctions.isEmpty() {
			if device.PCIeFunctions.isEmpty() && !strings.EqualFold(device.Status.State, "Absent") {
				partial = true
			}
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
				partial = true
				continue
			}
			objects = append(objects, object)
		}
		return objects, partial
	}
	if list.collection.ODataID != "" {
		objects, err := s.list(ctx, list.collection.ODataID)
		return objects, err != nil
	}
	return nil, false
}

func (s *redfishSession) list(ctx context.Context, link string) ([]redfishObject, error) {
	return s.listWithQuery(ctx, link, nil)
}

func (s *redfishSession) listProcessorGPUs(ctx context.Context, link string) ([]redfishObject, error) {
	query := make(url.Values)
	query.Set("$filter", "ProcessorType eq 'GPU'")
	query.Set("$expand", ".")

	processors, err := s.listWithQuery(ctx, link, query)
	if err == nil {
		return processors, nil
	}
	var statusError *redfishHTTPStatusError
	if !errors.As(err, &statusError) ||
		(statusError.statusCode != http.StatusBadRequest && statusError.statusCode != http.StatusNotImplemented) {
		return nil, err
	}

	// Query parameters are optional in Redfish. Retry with the plain collection
	// traversal when the BMC does not support filtering or expansion.
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
	for nextPage := link; nextPage != ""; {
		if _, seen := seenPages[nextPage]; seen {
			return objects, errors.New("redfish collection repeated a pagination link")
		}
		seenPages[nextPage] = struct{}{}

		var collection redfishCollection
		if err := s.get(ctx, nextPage, &collection); err != nil {
			return objects, err
		}
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
				return objects, err
			}
			objects = append(objects, object)
		}
		nextPage = collection.MembersNextLink
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
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

func redfishObjectID(object redfishObject) string {
	if object.ID != "" {
		return object.ID
	}
	return object.ODataID
}

func addGPU(inventory *hwmgrcontroller.GPUInventory, seen map[string]struct{}, gpu hwmgrcontroller.GPUInfo) {
	key := gpu.ID
	if gpu.SerialNumber != "" {
		key = strings.ToLower(gpu.SerialNumber)
	}
	if key == "" {
		return
	}
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	inventory.Devices = append(inventory.Devices, gpu)
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

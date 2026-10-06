// Copyright 2026 Hewlett Packard Enterprise Development LP

package driver

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/fake"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
)

// testSecrets mirrors the fake/fake/fake convention already used by controller_lunid_test.go.
var testSecrets = map[string]string{
	"backend":  "fake",
	"username": "fake",
	"password": "fake",
}

// countingExpandProvider wraps the vendored fake StorageProvider, counting ExpandVolume calls and
// simulating the real array's additive GrowVolume semantics (sizeMiB is added to the current size,
// not set to an absolute target — see the design doc's ControllerExpandVolume/GrowVolume row) so
// tests can detect silent over-provisioning from a duplicate request reaching the array twice
// (CON-4960-26/-27, R-02). Size is tracked independently in currentSize (not delegated to the
// embedded fake's internal map) because the vendored fake.StorageProvider.ExpandVolume mutates a
// local copy of its map entry and never persists it back — a pre-existing vendor limitation,
// worked around here rather than patched (vendor stays untouched).
type countingExpandProvider struct {
	*fake.StorageProvider
	expandCalls *int32
	currentSize *int64
}

func (p *countingExpandProvider) ExpandVolume(id string, requestBytes int64) (*model.Volume, error) {
	atomic.AddInt32(p.expandCalls, 1)
	newSize := atomic.AddInt64(p.currentSize, requestBytes)
	return p.StorageProvider.ExpandVolume(id, newSize)
}

func (p *countingExpandProvider) GetVolume(id string) (*model.Volume, error) {
	volume, err := p.StorageProvider.GetVolume(id)
	if err != nil || volume == nil {
		return volume, err
	}
	volume.Size = atomic.LoadInt64(p.currentSize)
	return volume, nil
}

// newDedupTestDriver builds a bare Driver registered with the given shared storage provider,
// optionally wired to a distributed dedup backend (nil simulates today's per-process-only
// behavior, pre-CON-4960-26).
func newDedupTestDriver(identity string, clientset *k8sfake.Clientset, provider storageprovider.StorageProvider) *Driver {
	driver := &Driver{
		name:             "test-driver",
		version:          "0.1",
		storageProviders: make(map[string]storageprovider.StorageProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
	}
	credential := &storageprovider.Credentials{Username: "fake", Backend: "fake"}
	driver.storageProviders[driver.GenerateStorageProviderCacheKey(credential)] = provider
	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	})
	if clientset != nil {
		driver.distributedDedup = newDistributedDedup(testDedupNamespace, identity, clientset, time.Minute)
	}
	return driver
}

func expandVolumeRequest(volumeID string, requiredBytes int64) *csi.ControllerExpandVolumeRequest {
	return &csi.ControllerExpandVolumeRequest{
		VolumeId:      volumeID,
		CapacityRange: &csi.CapacityRange{RequiredBytes: requiredBytes},
		Secrets:       testSecrets,
	}
}

// TestControllerExpandVolume_DuplicateWithoutDistributedDedup_DoubleExpands demonstrates today's
// bug (pre-CON-4960-26): a duplicate request that lands on a different controller pod has no
// shared state to detect it — each Driver's own per-process requestCache is empty for the other's
// completed request — so the array's additive GrowVolume-equivalent call happens twice.
func TestControllerExpandVolume_DuplicateWithoutDistributedDedup_DoubleExpands(t *testing.T) {
	var expandCalls int32
	currentSize := int64(1000)
	provider := &countingExpandProvider{StorageProvider: fake.NewFakeStorageProvider(), expandCalls: &expandCalls, currentSize: &currentSize}
	provider.CreateVolume("vol-1", "", 1000, nil)

	driver1 := newDedupTestDriver("pod-1", nil, provider)
	driver2 := newDedupTestDriver("pod-2", nil, provider)

	// Two sequential "duplicate" requests for the same volume, routed to two different
	// controller pods (independent requestCache per Driver) — both reach the array.
	_, err := driver1.ControllerExpandVolume(context.Background(), expandVolumeRequest("vol-1", 2000))
	assert.NoError(t, err)
	_, err = driver2.ControllerExpandVolume(context.Background(), expandVolumeRequest("vol-1", 2000))
	assert.NoError(t, err)

	assert.Equal(t, int32(2), atomic.LoadInt32(&expandCalls), "both pods' requests reached the array — the double-grow bug R-02 flags")
	volume, _ := provider.GetVolume("vol-1")
	assert.Equal(t, int64(5000), volume.Size, "silent over-provisioning: 1000 + 2000 + 2000 = 5000, instead of the intended 1000 + 2000 = 3000")
}

// TestControllerExpandVolume_DuplicateWithDistributedDedup_Prevented confirms CON-4960-26 closes
// the gap above: while driver1's request is still in flight (simulated by holding the dedup lock
// directly, rather than letting its RPC call complete and release it), driver2's duplicate is
// rejected before ever reaching the array.
func TestControllerExpandVolume_DuplicateWithDistributedDedup_Prevented(t *testing.T) {
	var expandCalls int32
	currentSize := int64(1000)
	provider := &countingExpandProvider{StorageProvider: fake.NewFakeStorageProvider(), expandCalls: &expandCalls, currentSize: &currentSize}
	provider.CreateVolume("vol-1", "", 1000, nil)

	clientset := k8sfake.NewSimpleClientset()
	driver1 := newDedupTestDriver("pod-1", clientset, provider)
	driver2 := newDedupTestDriver("pod-2", clientset, provider)

	assert.NoError(t, driver1.distributedDedup.TryAcquire(context.Background(), "ControllerExpandVolume:vol-1"))

	_, err := driver2.ControllerExpandVolume(context.Background(), expandVolumeRequest("vol-1", 2000))
	assert.Error(t, err, "driver2's duplicate must be rejected while driver1 holds the dedup lease")
	assert.Equal(t, codes.Aborted, status.Code(err))
	assert.Equal(t, int32(0), atomic.LoadInt32(&expandCalls), "ExpandVolume must never be reached for the rejected duplicate")

	assert.NoError(t, driver1.distributedDedup.Release(context.Background(), "ControllerExpandVolume:vol-1"))

	_, err = driver2.ControllerExpandVolume(context.Background(), expandVolumeRequest("vol-1", 2000))
	assert.NoError(t, err, "after release, driver2's retry must succeed — never permanently blocked")
	assert.Equal(t, int32(1), atomic.LoadInt32(&expandCalls))
	volume, _ := provider.GetVolume("vol-1")
	assert.Equal(t, int64(3000), volume.Size, "exactly one additive grow applied: 1000 + 2000 = 3000, no over-provisioning")
}

func createVolumeRequest(name string) *csi.CreateVolumeRequest {
	return &csi.CreateVolumeRequest{
		Name: name,
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
				AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
			},
		},
		Secrets: testSecrets,
	}
}

// TestCreateVolume_CrossPodDuplicateRejected confirms CreateVolume's HandleDuplicateRequest call
// (before any storage-provider interaction) correctly round-trips through the distributed dedup
// backend using its real key format ("CreateVolume:<name>").
func TestCreateVolume_CrossPodDuplicateRejected(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	driver1 := newDedupTestDriver("pod-1", clientset, fake.NewFakeStorageProvider())
	driver2 := newDedupTestDriver("pod-2", clientset, fake.NewFakeStorageProvider())

	assert.NoError(t, driver1.distributedDedup.TryAcquire(context.Background(), "CreateVolume:pvc-1"))

	_, err := driver2.CreateVolume(context.Background(), createVolumeRequest("pvc-1"))
	assert.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))

	assert.NoError(t, driver1.distributedDedup.Release(context.Background(), "CreateVolume:pvc-1"))

	_, err = driver2.CreateVolume(context.Background(), createVolumeRequest("pvc-1"))
	assert.NotEqual(t, codes.Aborted, status.Code(err), "after release, dedup must no longer block the retry")
}

func createSnapshotRequest(name, sourceVolumeID string) *csi.CreateSnapshotRequest {
	return &csi.CreateSnapshotRequest{
		Name:           name,
		SourceVolumeId: sourceVolumeID,
		Secrets:        testSecrets,
	}
}

// TestCreateSnapshot_CrossPodDuplicateRejected mirrors the CreateVolume test above, confirming
// CreateSnapshot's key format ("CreateSnapshot:<name>:<sourceVolumeId>").
func TestCreateSnapshot_CrossPodDuplicateRejected(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	driver1 := newDedupTestDriver("pod-1", clientset, fake.NewFakeStorageProvider())
	driver2 := newDedupTestDriver("pod-2", clientset, fake.NewFakeStorageProvider())

	key := "CreateSnapshot:snap-1:vol-1"
	assert.NoError(t, driver1.distributedDedup.TryAcquire(context.Background(), key))

	_, err := driver2.CreateSnapshot(context.Background(), createSnapshotRequest("snap-1", "vol-1"))
	assert.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))

	assert.NoError(t, driver1.distributedDedup.Release(context.Background(), key))

	_, err = driver2.CreateSnapshot(context.Background(), createSnapshotRequest("snap-1", "vol-1"))
	assert.NotEqual(t, codes.Aborted, status.Code(err), "after release, dedup must no longer block the retry")
}

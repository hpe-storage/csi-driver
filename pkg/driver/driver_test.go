// Copyright 2019 Hewlett Packard Enterprise Development LP
package driver

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Scalingo/go-etcd-lock/v5/lock"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/chapi"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/fake"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
	"github.com/kubernetes-csi/csi-test/pkg/sanity"
)

const (
	testsocket string = "/tmp/csi.sock"
	testKey    string = "testKey"
	testValue  string = "testValue"
	testCount  int    = 500
)

func TestPluginSuite(t *testing.T) {
	endpoint := "unix://" + testsocket
	if err := os.Remove(testsocket); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove unix domain socket file %s, error: %s", testsocket, err)
	}

	log.InitLogging("csi-test.log", &log.LogParams{Level: "trace"}, false)

	//driver := realDriver(t, endpoint)
	//secretsFile := "csi-secrets.yaml"
	driver := fakeDriver(endpoint)
	secretsFile := "fake-csi-secrets.yaml"
	driver.grpc = NewNonBlockingGRPCServer()
	// start node, controller and identity servers on same endpoint for tests
	go driver.grpc.Start(driver.endpoint, driver, driver, driver)
	defer driver.Stop(true)

	stagingPath := "./csi-mnt"
	targetPath := "./csi-mnt-stage"
	os.RemoveAll(stagingPath)
	os.RemoveAll(targetPath)

	config := &sanity.Config{
		StagingPath:     stagingPath,
		TargetPath:      targetPath,
		Address:         endpoint,
		SecretsFile:     secretsFile,
		CreateTargetDir: createTarget,
	}

	sanity.Test(t, config)
}

func createTarget(_ string) (string, error) {
	return "./csi-mnt-stage", nil
}

//nolint:unused
func realDriver(t *testing.T, endpoint string) *Driver {
	driver, err := NewDriver("test-driver", "0.1", endpoint, flavor.Kubernetes, true, "", "", false, 0, false, 0)

	if err != nil {
		t.Fatal("Failed to initialize driver")
	}
	return driver
}

func fakeDriver(endpoint string) *Driver {
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		endpoint:         endpoint,
		storageProviders: make(map[string]*cachedProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
	}

	credential := &storageprovider.Credentials{
		Username:    "fake",
		Backend:     "fake",
		ServiceName: "fake",
	}
	cacheKey := driver.GenerateStorageProviderCacheKey(credential)
	driver.storageProviders[cacheKey] = &cachedProvider{
		provider:   fake.NewFakeStorageProvider(),
		lastAccess: time.Now(),
	}

	driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		//// csi.ControllerServiceCapability_RPC_LIST_VOLUMES,  // TODO: UNCOMMENT THIS ONCE LIST FEATURE IS COMPLETELY SUPPORTED
		// csi.ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		//// csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS, // TODO: UNCOMMENT THIS ONCE LIST FEATURE IS COMPLETELY SUPPORTED
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	})

	driver.AddNodeServiceCapabilities([]csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
	})

	driver.AddPluginCapabilityVolumeExpansion([]csi.PluginCapability_VolumeExpansion_Type{
		csi.PluginCapability_VolumeExpansion_ONLINE,
	})

	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
	})

	return driver
}

func TestGenerateStorageProviderCacheKey(t *testing.T) {
	driver := &Driver{
		name:    "fake-test-driver",
		version: "0.1",
	}

	cred1 := &storageprovider.Credentials{
		Username:    "admin",
		Password:    "password",
		Backend:     "1.1.1.1",
		ServiceName: "fake",
	}
	cred2 := &storageprovider.Credentials{
		Username:    "user",
		Password:    "password",
		Backend:     "1.1.1.1",
		ServiceName: "fake",
	}
	cred3 := &storageprovider.Credentials{
		Username:    "test",
		Password:    "password",
		Backend:     "1.1.1.1",
		ServiceName: "fake",
	}
	cred4 := &storageprovider.Credentials{
		Username:    "test",
		Password:    "password",
		Backend:     "2.2.2.2",
		ServiceName: "fake",
	}
	cred5 := &storageprovider.Credentials{
		Username:    "user",
		Password:    "password",
		Backend:     "2.2.2.2",
		ServiceName: "fake",
	}

	type args struct {
		credential *storageprovider.Credentials
	}

	tests := []struct {
		name     string
		args     args
		expected string
		fails    bool
	}{
		{"Test for user1", args{cred1}, "10ea735ee58e2a9ce159ee7d5e9439c05da47b2f91c85d4dcb5152c3295390ba", false},
		{"Test for user1", args{cred1}, "10ea735ee58e2a9ce159ee7d5e9439c05da47b2f91c85d4dcb5152c3295390ba", true},
		{"Test for user2", args{cred2}, "4d1207ac0389f6b9d593e3d8bdbf610e5441ca9679d85380cea9a9ea31bb2292", false},
		{"Test for user3", args{cred3}, "5729f10afcd4e1868f2f1351861012354c9989eefd971c489f44f7c7583be4b8", false},
		{"Test for user4", args{cred4}, "99cc4ecbf4fbe1ae762f6971752663e41a2701bc1cb3d4159a389e6ad799c984", false},
		{"Test for user5", args{cred5}, "9b882f7526e28c59e9507b07543fcad084d52597935d92e4815d3d086237a0a4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := driver.GenerateStorageProviderCacheKey(tt.args.credential); got != tt.expected {
				if tt.fails == false {
					t.Errorf("GenerateStorageProviderCacheKey() = %v, want %v", got, tt.expected)
				}
			}
		})
	}
}

// newEmptyStorageProviderDriver returns a Driver with an initialized, empty
// storageProviders map for storage-provider cache tests (DRV-A1).
func newEmptyStorageProviderDriver() *Driver {
	return &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		storageProviders: make(map[string]*cachedProvider),
	}
}

// addFakeProvider inserts a fake storage provider for the given credentials
// directly into the (mutex-protected) map and returns its cache key.
func addFakeProvider(driver *Driver, cred *storageprovider.Credentials) string {
	cacheKey := driver.GenerateStorageProviderCacheKey(cred)
	driver.spMutex.Lock()
	driver.storageProviders[cacheKey] = &cachedProvider{
		provider:   fake.NewFakeStorageProvider(),
		lastAccess: time.Now(),
	}
	driver.spMutex.Unlock()
	return cacheKey
}

// TestGetStorageProviderCount verifies that getStorageProviderCount returns the
// number of cached storage providers (DRV-A1).
func TestGetStorageProviderCount(t *testing.T) {
	driver := newEmptyStorageProviderDriver()
	assert.Equal(t, 0, driver.getStorageProviderCount(), "expected no providers initially")

	addFakeProvider(driver, &storageprovider.Credentials{Username: "u1", Backend: "1.1.1.1", ServiceName: "fake"})
	assert.Equal(t, 1, driver.getStorageProviderCount(), "expected one provider after add")

	addFakeProvider(driver, &storageprovider.Credentials{Username: "u2", Backend: "2.2.2.2", ServiceName: "fake"})
	assert.Equal(t, 2, driver.getStorageProviderCount(), "expected two providers after second add")
}

// TestGetStorageProviders verifies that getStorageProviders returns a snapshot
// slice containing all cached storage providers (DRV-A1).
func TestGetStorageProviders(t *testing.T) {
	driver := newEmptyStorageProviderDriver()
	assert.Empty(t, driver.getStorageProviders(), "expected empty snapshot initially")

	addFakeProvider(driver, &storageprovider.Credentials{Username: "u1", Backend: "1.1.1.1", ServiceName: "fake"})
	addFakeProvider(driver, &storageprovider.Credentials{Username: "u2", Backend: "2.2.2.2", ServiceName: "fake"})

	providers := driver.getStorageProviders()
	assert.Len(t, providers, 2, "snapshot should contain both providers")
	for _, sp := range providers {
		assert.NotNil(t, sp, "snapshot should not contain nil providers")
	}
}

// TestRemoveStorageProvider verifies that RemoveStorageProvider removes the
// cached provider and is a safe no-op for unknown credentials (DRV-A1).
func TestRemoveStorageProvider(t *testing.T) {
	driver := newEmptyStorageProviderDriver()
	cred := &storageprovider.Credentials{Username: "u1", Backend: "1.1.1.1", ServiceName: "fake"}
	addFakeProvider(driver, cred)
	assert.Equal(t, 1, driver.getStorageProviderCount())

	driver.RemoveStorageProvider(cred)
	assert.Equal(t, 0, driver.getStorageProviderCount(), "provider should be removed")

	// Removing again (unknown key) must not panic and must remain a no-op.
	assert.NotPanics(t, func() { driver.RemoveStorageProvider(cred) })
	assert.Equal(t, 0, driver.getStorageProviderCount())
}

// TestStorageProvidersConcurrentAccess exercises the storageProviders map from
// many goroutines simultaneously. Before the DRV-A1 fix this would trigger a
// fatal "concurrent map read and map write" panic; with the sync.RWMutex in
// place it must run cleanly. Run with `go test -race` to detect data races.
func TestStorageProvidersConcurrentAccess(t *testing.T) {
	driver := newEmptyStorageProviderDriver()

	const goroutines = 50
	const iterations = 100

	creds := make([]*storageprovider.Credentials, 10)
	for i := range creds {
		creds[i] = &storageprovider.Credentials{
			Username:    "user" + strconv.Itoa(i),
			Backend:     strconv.Itoa(i) + "." + strconv.Itoa(i) + "." + strconv.Itoa(i) + "." + strconv.Itoa(i),
			ServiceName: "fake",
		}
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cred := creds[(g+i)%len(creds)]
				switch i % 5 {
				case 0:
					addFakeProvider(driver, cred)
				case 1:
					_ = driver.getStorageProviders()
				case 2:
					_ = driver.getStorageProviderCount()
				case 3:
					driver.RemoveStorageProvider(cred)
				case 4:
					_ = driver.GenerateStorageProviderCacheKey(cred)
				}
			}
		}(g)
	}
	wg.Wait()

	// The map must still be usable and consistent after concurrent access.
	assert.True(t, driver.getStorageProviderCount() >= 0)
	assert.NotPanics(t, func() { _ = driver.getStorageProviders() })
}

func TestAddRequest(t *testing.T) {
	driver := &Driver{}

	key := testKey
	value := testValue

	driver.AddRequest(key, value)

	assert.Equal(t, value, driver.GetRequest(key))
}

func TestAddRequestConcurrent(t *testing.T) {
	driver := &Driver{}

	key := testKey
	value := testValue

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			driver.AddRequest(key, value)
		}()
	}
	wg.Wait()

	assert.Equal(t, value, driver.GetRequest(key))
}

func TestClearRequest(t *testing.T) {
	driver := &Driver{}

	key := testKey
	value := testValue

	driver.AddRequest(key, value)
	driver.ClearRequest(key)

	assert.Nil(t, driver.GetRequest(key))
}

func TestAddAndClearRequestConcurrent(_ *testing.T) {
	driver := &Driver{}

	var key [testCount]string
	var value [testCount]string
	for i := 0; i < (testCount - 5); i += 5 {
		key[i] = testKey + strconv.Itoa(i)
		value[i] = testValue + strconv.Itoa(i)
		key[i+1] = testKey + strconv.Itoa(i)
		value[i+1] = testValue + strconv.Itoa(i)
		key[i+2] = testKey + strconv.Itoa(i)
		value[i+2] = testValue + strconv.Itoa(i)
		key[i+3] = testKey + strconv.Itoa(i)
		value[i+3] = testValue + strconv.Itoa(i)
		key[i+4] = testKey + strconv.Itoa(i)
		value[i+4] = testValue + strconv.Itoa(i)
	}

	var wg sync.WaitGroup
	for i := 0; i < testCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			driver.AddRequest(key[i], value[i])
			driver.HandleDuplicateRequest(key[i])
			rand.Seed(time.Now().UnixNano())
			n := rand.Intn(1000) // n will be between 0 and 10
			time.Sleep(time.Duration(n) * time.Microsecond)
			driver.ClearRequest(key[i])
		}(i)
	}
	wg.Wait()

}

func TestClearRequestConcurrent(_ *testing.T) {
	driver := &Driver{}

	var key [testCount]string
	var value [testCount]string
	for i := 0; i < (testCount - 5); i += 5 {
		key[i] = testKey + strconv.Itoa(i)
		value[i] = testValue + strconv.Itoa(i)
		key[i+1] = testKey + strconv.Itoa(i)
		value[i+1] = testValue + strconv.Itoa(i)
		key[i+2] = testKey + strconv.Itoa(i)
		value[i+2] = testValue + strconv.Itoa(i)
		key[i+3] = testKey + strconv.Itoa(i)
		value[i+3] = testValue + strconv.Itoa(i)
		key[i+4] = testKey + strconv.Itoa(i)
		value[i+4] = testValue + strconv.Itoa(i)
	}

	var wg sync.WaitGroup
	for i := 0; i < testCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			driver.HandleDuplicateRequest(key[i])
			rand.Seed(time.Now().UnixNano())
			n := rand.Intn(1000) // n will be between 0 and 10
			time.Sleep(time.Duration(n) * time.Microsecond)
			driver.ClearRequest(key[i])
		}(i)
	}
	wg.Wait()

}

// --- DRV-B3: in-memory requestCache TTL / stale-entry self-healing tests ---

// TestHandleDuplicateRequest_InFlightIsAborted verifies that a second request
// for the same key, while the first is still in-flight (within TTL), is
// rejected with codes.Aborted.
func TestHandleDuplicateRequest_InFlightIsAborted(t *testing.T) {
	driver := &Driver{}

	key := "CreateVolume:pvc-inflight"

	// First request claims the key.
	assert.NoError(t, driver.HandleDuplicateRequest(key))

	// Second concurrent request for the same key must be aborted.
	err := driver.HandleDuplicateRequest(key)
	assert.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))

	// After clearing, the key can be acquired again.
	driver.ClearRequest(key)
	assert.NoError(t, driver.HandleDuplicateRequest(key))
}

// TestHandleDuplicateRequest_StaleEntryReclaimed verifies the DRV-B3 fix: if a
// previous request left an entry behind (e.g. a panic prevented ClearRequest
// from running), a later request for the same key reclaims the stale guard once
// it exceeds the TTL instead of returning ABORTED forever.
func TestHandleDuplicateRequest_StaleEntryReclaimed(t *testing.T) {
	driver := &Driver{
		// Use a tiny TTL so the entry becomes stale almost immediately.
		requestCacheTTL: 20 * time.Millisecond,
	}

	key := "CreateVolume:pvc-stale"

	// Simulate a request that acquired the guard but never called ClearRequest.
	assert.NoError(t, driver.HandleDuplicateRequest(key))

	// Immediately after, a duplicate must still be blocked (within TTL).
	err := driver.HandleDuplicateRequest(key)
	assert.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))

	// Wait for the guard to exceed its TTL, then a new request must succeed —
	// the stale entry is reclaimed rather than blocking forever.
	time.Sleep(40 * time.Millisecond)
	assert.NoError(t, driver.HandleDuplicateRequest(key),
		"stale in-memory request guard should be reclaimed after TTL (DRV-B3)")

	driver.ClearRequest(key)
}

// TestHandleDuplicateRequest_FreshEntryNotReclaimed ensures the TTL logic does
// not prematurely reclaim a guard that is still within its TTL (no false
// "not duplicate" results for genuine in-flight requests).
func TestHandleDuplicateRequest_FreshEntryNotReclaimed(t *testing.T) {
	driver := &Driver{
		requestCacheTTL: 10 * time.Second, // large TTL — entry stays valid
	}

	key := "DeleteVolume:pvc-fresh"

	assert.NoError(t, driver.HandleDuplicateRequest(key))

	// Several quick retries should all be aborted because the guard is fresh.
	for i := 0; i < 5; i++ {
		err := driver.HandleDuplicateRequest(key)
		assert.Error(t, err)
		assert.Equal(t, codes.Aborted, status.Code(err))
	}
}

// TestHandleDuplicateRequest_DefaultTTL verifies the default TTL is used when
// the override is not set, and that the guard is treated as in-flight.
func TestHandleDuplicateRequest_DefaultTTL(t *testing.T) {
	driver := &Driver{} // requestCacheTTL == 0 => default inMemoryRequestTTL

	assert.Equal(t, inMemoryRequestTTL, driver.inMemoryRequestCacheTTL())

	key := "ExpandVolume:pvc-default"
	assert.NoError(t, driver.HandleDuplicateRequest(key))
	err := driver.HandleDuplicateRequest(key)
	assert.Error(t, err)
	assert.Equal(t, codes.Aborted, status.Code(err))
}

// TestHandleDuplicateRequest_ConcurrentStaleReclaim ensures that when many
// goroutines race to reclaim the same stale entry, exactly one wins (acquires
// the key) and the rest are aborted — the CompareAndSwap keeps reclamation
// race-free.
//
// The test is made deterministic (including under -race) by:
//   - Using a large TTL so the *fresh* guard written by the winning goroutine
//     never ages out during the test window, and
//   - Seeding a guard whose createdAt is already well in the past so it is
//     unambiguously stale before the goroutines start.
//
// Under these conditions exactly one goroutine can CompareAndSwap the single
// stale value; every other goroutine observes either the stale guard (and loses
// the CAS) or the winner's fresh, in-TTL guard, and is therefore aborted.
func TestHandleDuplicateRequest_ConcurrentStaleReclaim(t *testing.T) {
	driver := &Driver{
		requestCacheTTL: 1 * time.Hour, // large: the fresh guard never goes stale mid-test
	}

	key := "CreateVolume:pvc-race"

	// Seed an already-stale guard directly (createdAt far in the past).
	driver.requestCache.Store(key, &requestGuard{createdAt: time.Now().Add(-2 * time.Hour)})

	const goroutines = 50
	var (
		wg         sync.WaitGroup
		successes  int32
		abortedCnt int32
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if err := driver.HandleDuplicateRequest(key); err != nil {
				atomic.AddInt32(&abortedCnt, 1)
			} else {
				atomic.AddInt32(&successes, 1)
			}
		}()
	}
	wg.Wait()

	// Exactly one goroutine should have reclaimed the stale entry.
	assert.Equal(t, int32(1), atomic.LoadInt32(&successes),
		"exactly one goroutine should reclaim the stale guard")
	assert.Equal(t, int32(goroutines-1), atomic.LoadInt32(&abortedCnt))

	driver.ClearRequest(key)
}

// fakeLock is a no-op lock.Lock used by fakeDBService for tests.
type fakeLock struct{}

func (fakeLock) Release() error { return nil }

// fakeDBService is a minimal in-memory implementation of dbservice.DBService for
// unit tests. It is intentionally simple: it backs Get/Put/Delete with a map and
// tracks lock state so the driver's DB-backed request and entry bookkeeping can
// be exercised without a real etcd backend.
type fakeDBService struct {
	mu     sync.Mutex
	store  map[string]string
	locked map[string]bool
}

func newFakeDBService() *fakeDBService {
	return &fakeDBService{
		store:  make(map[string]string),
		locked: make(map[string]bool),
	}
}

func (db *fakeDBService) Get(key string) (*string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	val, ok := db.store[key]
	if !ok {
		return nil, nil
	}
	v := val
	return &v, nil
}

func (db *fakeDBService) Put(key string, value string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.store[key] = value
	return nil
}

func (db *fakeDBService) PutWithLeaseExpiry(key string, value string, _ int64) error {
	return db.Put(key, value)
}

func (db *fakeDBService) Delete(key string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.store, key)
	return nil
}

func (db *fakeDBService) IsLocked(key string) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.locked[key], nil
}

func (db *fakeDBService) AcquireLock(key string, _ int) (lock.Lock, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.locked[key] = true
	return fakeLock{}, nil
}

func (db *fakeDBService) WaitAcquireLock(key string, _ int) (lock.Lock, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.locked[key] = true
	return fakeLock{}, nil
}

func (db *fakeDBService) ReleaseLock(_ lock.Lock) error { return nil }

// hasKey reports whether the given key currently exists in the fake DB store.
func (db *fakeDBService) hasKey(key string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, ok := db.store[key]
	return ok
}

// TestCreateSnapshot_RemovesPendingDBEntryOnFailure is a regression test for
// DRV-D1. CreateSnapshot creates a DB entry keyed by request.Name (dbKey) and
// must clean that entry up on the failure path. Previously the deferred
// RemoveFromDBIfPending was called with the duplicate-request key instead of
// dbKey, leaving the request.Name entry stuck in PENDING forever and causing
// subsequent snapshot creation for the same name to fail.
//
// The test forces CreateSnapshot to fail immediately after the DB entry is
// created (empty secrets make GetStorageProvider fail) and asserts that the
// PENDING entry stored under request.Name was removed.
func TestCreateSnapshot_RemovesPendingDBEntryOnFailure(t *testing.T) {
	db := newFakeDBService()
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		storageProviders: make(map[string]*cachedProvider),
		flavor:           &vanilla.Flavor{},
		DBService:        db,
	}

	const snapName = "snap-drv-d1"
	request := &csi.CreateSnapshotRequest{
		Name:           snapName,
		SourceVolumeId: "pvc-source-volume",
		// No secrets: GetStorageProvider will fail, exercising the failure path
		// that runs the deferred RemoveFromDBIfPending(dbKey).
		Secrets: map[string]string{},
	}

	_, err := driver.CreateSnapshot(context.Background(), request)
	assert.Error(t, err, "CreateSnapshot should fail when secrets are missing")

	// The PENDING entry is keyed by request.Name (dbKey). With the DRV-D1 fix it
	// must be removed; the duplicate-request key must also not linger.
	assert.False(t, db.hasKey(snapName),
		"PENDING DB entry keyed by request.Name must be cleaned up on failure (DRV-D1)")

	dupKey := fmt.Sprintf("%s:%s:%s", "CreateSnapshot", request.Name, request.SourceVolumeId)
	assert.False(t, db.hasKey(dupKey),
		"duplicate-request key should never have been written to the DB store")
}

// publishedVolumeProvider embeds the fake storage provider and overrides
// GetVolume to report a single, still-published volume. It is used to exercise
// the DeleteVolume "volume in use" path without a real backend.
type publishedVolumeProvider struct {
	*fake.StorageProvider
	volumeID string
}

func (p *publishedVolumeProvider) GetVolume(id string) (*model.Volume, error) {
	if id == p.volumeID {
		return &model.Volume{
			ID:        p.volumeID,
			Name:      p.volumeID,
			Published: true,
		}, nil
	}
	return nil, nil
}

// TestDeleteVolume_PublishedReturnsFailedPrecondition is a regression test for
// DRV-D2. When a volume is still published (in use), DeleteVolume must return
// codes.FailedPrecondition per the CSI spec so the external provisioner stops
// retrying, rather than codes.Internal which triggers aggressive retries.
func TestDeleteVolume_PublishedReturnsFailedPrecondition(t *testing.T) {
	const volumeID = "pvc-published-drv-d2"

	driver := &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		storageProviders: make(map[string]*cachedProvider),
		flavor:           &vanilla.Flavor{},
	}

	// Insert a provider that reports the volume as published. DeleteVolume with
	// no secrets searches all cached providers via GetVolumeByID.
	driver.storageProviders["fake"] = &cachedProvider{
		provider: &publishedVolumeProvider{
			StorageProvider: fake.NewFakeStorageProvider(),
			volumeID:        volumeID,
		},
		lastAccess: time.Now(),
	}

	_, err := driver.DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{
		VolumeId: volumeID,
	})

	assert.Error(t, err, "DeleteVolume should fail for a published volume")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"published (in-use) volume must return FailedPrecondition per CSI spec (DRV-D2)")
}

// TestResolveAccessProtocol is a regression test for DRV-D6. The access
// protocol selection logic must default an empty value to iSCSI, accept only
// the supported block protocols (iscsi, fc, nvmetcp) in a case-insensitive,
// whitespace-tolerant manner, and reject every other value with an
// InvalidArgument error instead of silently forwarding it to the CSP.
func TestResolveAccessProtocol(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		// Defaulting: empty / whitespace-only input defaults to iSCSI.
		{name: "empty defaults to iscsi", input: "", expected: iscsi},
		{name: "whitespace defaults to iscsi", input: "   ", expected: iscsi},

		// Supported protocols (canonical lowercase).
		{name: "iscsi", input: "iscsi", expected: iscsi},
		{name: "fc", input: "fc", expected: fc},
		{name: "nvmetcp", input: "nvmetcp", expected: nvmetcp},

		// Case-insensitive and whitespace tolerant.
		{name: "iscsi uppercase", input: "ISCSI", expected: iscsi},
		{name: "fc mixed case", input: "Fc", expected: fc},
		{name: "nvmetcp uppercase", input: "NVMETCP", expected: nvmetcp},
		{name: "iscsi padded", input: "  iscsi  ", expected: iscsi},

		// "nvmeotcp" is an accepted synonym normalized to canonical nvmetcp.
		{name: "nvmeotcp synonym", input: "nvmeotcp", expected: nvmetcp},
		{name: "nvmeotcp uppercase synonym", input: "NVMEoTCP", expected: nvmetcp},

		// Unsupported protocols must be rejected.
		{name: "fcoe rejected", input: "fcoe", wantErr: true},
		{name: "nvme rejected", input: "nvme", wantErr: true},
		{name: "nvme-tcp variant rejected", input: "nvme-tcp", wantErr: true},
		{name: "nfs rejected", input: "nfs", wantErr: true},
		{name: "garbage rejected", input: "not-a-protocol", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveAccessProtocol(tc.input)
			if tc.wantErr {
				assert.Error(t, err, "expected unsupported protocol %q to be rejected", tc.input)
				assert.Equal(t, codes.InvalidArgument, status.Code(err),
					"unsupported protocol must map to InvalidArgument (DRV-D6)")
				assert.Empty(t, got, "no protocol should be returned on error")
				return
			}
			assert.NoError(t, err, "protocol %q should be accepted", tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// TestNormalizeAccessProtocol is a regression test for CROSS-3. The driver must
// fold every accepted spelling/casing of a block access protocol onto a single
// canonical constant so that the "nvmeotcp" synonym and the canonical
// "nvmetcp" value are treated identically by every downstream comparison
// (publishContext population, node-side setupDevice, PV-expand rescan, etc.).
// Unlike resolveAccessProtocol, this helper does not validate or default; it
// only canonicalizes, so unknown values pass through trimmed/lowercased.
func TestNormalizeAccessProtocol(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// NVMe/TCP: canonical spelling and the "nvmeotcp" synonym both fold onto
		// the canonical nvmetcp constant, case- and whitespace-insensitively.
		{name: "nvmetcp canonical", input: "nvmetcp", expected: nvmetcp},
		{name: "nvmeotcp synonym", input: "nvmeotcp", expected: nvmetcp},
		{name: "nvmetcp uppercase", input: "NVMETCP", expected: nvmetcp},
		{name: "nvmeotcp uppercase synonym", input: "NVMEoTCP", expected: nvmetcp},
		{name: "nvmeotcp padded synonym", input: "  nvmeotcp  ", expected: nvmetcp},

		// iSCSI and FC normalize to their canonical lowercase constants.
		{name: "iscsi", input: "iscsi", expected: iscsi},
		{name: "iscsi uppercase", input: "ISCSI", expected: iscsi},
		{name: "fc mixed case", input: "Fc", expected: fc},

		// Pass-through (no validation here): unknown stays trimmed/lowercased and
		// is never mistaken for NVMe/TCP. Empty stays empty (defaulting is the
		// job of resolveAccessProtocol, not normalizeAccessProtocol).
		{name: "nfs passthrough", input: "NFS", expected: nfsFileSystem},
		{name: "unknown passthrough", input: "  NVME  ", expected: "nvme"},
		{name: "empty stays empty", input: "", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, normalizeAccessProtocol(tc.input),
				"normalizeAccessProtocol(%q) must canonicalize to %q (CROSS-3)", tc.input, tc.expected)
		})
	}

	// The "nvmeotcp" synonym and the canonical "nvmetcp" constant must collapse
	// to the exact same value so that node-side "== nvmetcp" checks succeed
	// regardless of which spelling the CSP/StorageClass used.
	assert.Equal(t, normalizeAccessProtocol(nvmeotcp), normalizeAccessProtocol(nvmetcp),
		"the nvmeotcp synonym must normalize identically to the canonical nvmetcp constant (CROSS-3)")
}

// TestControllerExpandVolume_MissingVolumeReturnsErrorNoPanic is a regression
// test for DRV-E4. When the volume to expand cannot be found, ControllerExpandVolume
// must return a clean error rather than dereferencing a nil *model.Volume and
// panicking. With no storage providers registered, GetVolumeByID resolves the
// "pvc-" volume to NotFound; the handler must surface that error without panic.
func TestControllerExpandVolume_MissingVolumeReturnsErrorNoPanic(t *testing.T) {
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		storageProviders: make(map[string]*cachedProvider),
		flavor:           &vanilla.Flavor{},
	}

	request := &csi.ControllerExpandVolumeRequest{
		VolumeId:      "pvc-does-not-exist-drv-e4",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 20 * giB},
	}

	var (
		resp *csi.ControllerExpandVolumeResponse
		err  error
	)
	// The core DRV-E4 guarantee: no nil-pointer panic on a missing volume.
	assert.NotPanics(t, func() {
		resp, err = driver.ControllerExpandVolume(context.Background(), request)
	}, "ControllerExpandVolume must not panic when the volume is missing (DRV-E4)")

	assert.Error(t, err, "expanding a missing volume must return an error")
	assert.Nil(t, resp, "no response should be returned on error")
	assert.Equal(t, codes.NotFound, status.Code(err),
		"a missing volume must map to NotFound")
}

// TestControllerExpandVolume_ExistingVolumePassesNilGuard verifies that the
// DRV-E4 nil-guard does not regress the normal path: a real (non-nil) volume
// must flow past the guard. Requesting the volume's current size triggers the
// "already at requested size" early return, which exercises the field accesses
// guarded by the nil-check (Published, AccessProtocol, Size) without requiring a
// live storage backend.
func TestControllerExpandVolume_ExistingVolumePassesNilGuard(t *testing.T) {
	const volumeID = "pvc-expand-existing-drv-e4"
	const currentSize = 10 * giB

	driver := &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		storageProviders: make(map[string]*cachedProvider),
		flavor:           &vanilla.Flavor{},
	}

	// Seed a fake provider that already holds the volume at currentSize.
	sp := fake.NewFakeStorageProvider()
	if _, err := sp.CreateVolume(volumeID, "", currentSize, map[string]interface{}{}); err != nil {
		t.Fatalf("failed to seed fake volume: %v", err)
	}
	driver.storageProviders["fake"] = &cachedProvider{provider: sp, lastAccess: time.Now()}

	// Requesting the current size hits the idempotent "already at requested size"
	// early return, which is reached only after passing the nil-guard.
	request := &csi.ControllerExpandVolumeRequest{
		VolumeId:      volumeID,
		CapacityRange: &csi.CapacityRange{RequiredBytes: currentSize},
	}

	resp, err := driver.ControllerExpandVolume(context.Background(), request)
	assert.NoError(t, err, "expanding an existing volume to its current size should succeed")
	assert.NotNil(t, resp, "a response is expected for a valid volume")
	assert.Equal(t, int64(currentSize), resp.CapacityBytes,
		"capacity should reflect the volume's current size")
}

// TestValidateNoDualStack is a regression test for CROSS-1. Backend discovery
// addresses must all belong to a single IP family; a mix of IPv4 and IPv6
// (dual-stack) must be rejected. Single-family lists, empty lists, and lists
// containing non-IP entries (e.g. FC target names) must be accepted.
func TestValidateNoDualStack(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		wantErr   bool
	}{
		{name: "empty", addresses: nil},
		{name: "single ipv4", addresses: []string{"10.0.0.1"}},
		{name: "multiple ipv4", addresses: []string{"10.0.0.1", "10.0.0.2"}},
		{name: "single ipv6", addresses: []string{"fd00::1"}},
		{name: "multiple ipv6", addresses: []string{"fd00::1", "fd00::2"}},
		{name: "bracketed ipv6", addresses: []string{"[fd00::1]", "[fd00::2]"}},
		{name: "non-ip entries ignored", addresses: []string{"iqn.2003-10.com.3pardata:fake", ""}},
		{name: "ipv4 with non-ip entries", addresses: []string{"10.0.0.1", "not-an-ip"}},
		// Dual-stack combinations must be rejected.
		{name: "ipv4 and ipv6", addresses: []string{"10.0.0.1", "fd00::1"}, wantErr: true},
		{name: "ipv6 and ipv4", addresses: []string{"fd00::1", "10.0.0.1"}, wantErr: true},
		{name: "bracketed ipv6 with ipv4", addresses: []string{"[fd00::1]", "10.0.0.2"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNoDualStack(tc.addresses)
			if tc.wantErr {
				assert.Error(t, err, "expected dual-stack addresses %v to be rejected", tc.addresses)
				return
			}
			assert.NoError(t, err, "single-family addresses %v should be accepted", tc.addresses)
		})
	}
}

// TestValidateProtocolDiscoveryIPs is a regression test for CROSS-1. NVMe/TCP
// supports IPv4 discovery addresses only; an IPv6 discovery address must be
// rejected for nvmetcp. Other protocols (iscsi, fc) impose no family
// constraint here.
func TestValidateProtocolDiscoveryIPs(t *testing.T) {
	tests := []struct {
		name      string
		protocol  string
		addresses []string
		wantErr   bool
	}{
		{name: "nvmetcp ipv4 ok", protocol: nvmetcp, addresses: []string{"10.0.0.1"}},
		{name: "nvmetcp multiple ipv4 ok", protocol: nvmetcp, addresses: []string{"10.0.0.1", "10.0.0.2"}},
		{name: "nvmetcp ipv6 rejected", protocol: nvmetcp, addresses: []string{"fd00::1"}, wantErr: true},
		{name: "nvmetcp bracketed ipv6 rejected", protocol: nvmetcp, addresses: []string{"[fd00::1]"}, wantErr: true},
		{name: "nvmetcp case-insensitive ipv6 rejected", protocol: "NVMETCP", addresses: []string{"fd00::1"}, wantErr: true},
		// Non-NVMe/TCP protocols are not constrained by this check.
		{name: "iscsi ipv6 allowed", protocol: iscsi, addresses: []string{"fd00::1"}},
		{name: "fc no discovery ips", protocol: fc, addresses: nil},
		{name: "nvmetcp empty ok", protocol: nvmetcp, addresses: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProtocolDiscoveryIPs(tc.protocol, tc.addresses)
			if tc.wantErr {
				assert.Error(t, err, "expected protocol %s with addresses %v to be rejected", tc.protocol, tc.addresses)
				return
			}
			assert.NoError(t, err, "protocol %s with addresses %v should be accepted", tc.protocol, tc.addresses)
		})
	}
}

// TestIsReplicationRequested verifies the driver's detection of array-based
// replication requests from StorageClass/PVC create parameters (CROSS-2).
func TestIsReplicationRequested(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]string
		want       bool
	}{
		{name: "nil params", parameters: nil},
		{name: "empty params", parameters: map[string]string{}},
		{name: "remoteCopyGroup set", parameters: map[string]string{remoteCopyGroupKey: "rcg-1"}, want: true},
		{name: "remoteCopyGroup empty", parameters: map[string]string{remoteCopyGroupKey: ""}},
		{name: "remoteCopyGroup whitespace", parameters: map[string]string{remoteCopyGroupKey: "   "}},
		{name: "replicationDevices set", parameters: map[string]string{replicationDevicesKey: "rep-crd"}, want: true},
		{name: "oneRcgPerPvc true", parameters: map[string]string{oneRcgPerPvcKey: "true"}, want: true},
		{name: "oneRcgPerPvc True mixed case", parameters: map[string]string{oneRcgPerPvcKey: "True"}, want: true},
		{name: "oneRcgPerPvc false", parameters: map[string]string{oneRcgPerPvcKey: "false"}},
		{name: "oneRcgPerPvc garbage", parameters: map[string]string{oneRcgPerPvcKey: "notabool"}},
		{name: "allowBatch true", parameters: map[string]string{allowBatchReplicatedVolumeCreationKey: "true"}, want: true},
		{name: "allowBatch false", parameters: map[string]string{allowBatchReplicatedVolumeCreationKey: "false"}},
		{name: "unrelated params", parameters: map[string]string{accessProtocolKey: iscsi, "cpg": "x"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isReplicationRequested(tc.parameters))
		})
	}
}

// TestValidateReplicationProtocolCompatibility is a regression test for
// CROSS-2: combining array-based replication with NVMe/TCP must be rejected by
// the CSI driver with an InvalidArgument error, while supported combinations
// and non-replicated requests pass.
func TestValidateReplicationProtocolCompatibility(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]string
		wantErr    bool
		wantCode   codes.Code
	}{
		{
			name:       "no replication, nvmetcp ok",
			parameters: map[string]string{accessProtocolKey: nvmetcp},
		},
		{
			name:       "replication with iscsi ok",
			parameters: map[string]string{remoteCopyGroupKey: "rcg-1", accessProtocolKey: iscsi},
		},
		{
			name:       "replication with fc ok",
			parameters: map[string]string{replicationDevicesKey: "rep-crd", accessProtocolKey: fc},
		},
		{
			name:       "replication with default protocol ok",
			parameters: map[string]string{remoteCopyGroupKey: "rcg-1"},
		},
		{
			name:       "remoteCopyGroup with nvmetcp rejected",
			parameters: map[string]string{remoteCopyGroupKey: "rcg-1", accessProtocolKey: nvmetcp},
			wantErr:    true,
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "replicationDevices with nvmetcp rejected",
			parameters: map[string]string{replicationDevicesKey: "rep-crd", accessProtocolKey: nvmetcp},
			wantErr:    true,
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "oneRcgPerPvc with nvmetcp rejected",
			parameters: map[string]string{oneRcgPerPvcKey: "true", accessProtocolKey: nvmetcp},
			wantErr:    true,
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "replication with nvmeotcp synonym rejected",
			parameters: map[string]string{remoteCopyGroupKey: "rcg-1", accessProtocolKey: nvmeotcp},
			wantErr:    true,
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "replication with NVMETCP uppercase rejected",
			parameters: map[string]string{remoteCopyGroupKey: "rcg-1", accessProtocolKey: "NVMETCP"},
			wantErr:    true,
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "replication with invalid protocol rejected",
			parameters: map[string]string{remoteCopyGroupKey: "rcg-1", accessProtocolKey: "bogus"},
			wantErr:    true,
			wantCode:   codes.InvalidArgument,
		},
		{
			name:       "invalid protocol without replication is allowed here",
			parameters: map[string]string{accessProtocolKey: "bogus"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReplicationProtocolCompatibility(tc.parameters)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tc.wantCode, status.Code(err))
				return
			}
			assert.NoError(t, err)
		})
	}
}

// Copyright 2020 Hewlett Packard Enterprise Development LP
package driver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/concurrent"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultVolumeID = "testVolID"
const defaultTargetPath = "/mnt/test"
const defaultStagingPath = "/staging"

func TestNodeGetIntEnv(t *testing.T) {
	driver := &Driver{
		name:    "fake-test-driver",
		version: "0.1",
	}
	os.Setenv("TEST1", "-1")
	os.Setenv("TEST2", "100")
	os.Setenv("TEST3", "one")
	os.Setenv("TEST4", "0")

	tests := []struct {
		name     string
		args     string
		expected int64
	}{
		{"Test for env1", "TEST1", -1},
		{"Test for env2", "TEST2", 100},
		{"Test for env3", "TEST3", 0},
		{"Test for env4", "TEST4", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := driver.nodeGetIntEnv(tt.args); got != tt.expected {
				t.Errorf("nodeGetIntEnv() = %v, want %v", got, tt.expected)

			}
		})
	}

}

func TestKubeletRootDir(t *testing.T) {
	endpoint := "unix://" + testsocket
	volumeID := "1"
	testCases := []struct {
		name           string
		kubeletRootDir string
		expectVal      string
	}{
		{
			name:           "default path",
			kubeletRootDir: DefaultKubeletRoot,
			expectVal:      DefaultKubeletRoot + DefaultPluginMountPath + "/" + volumeID,
		},
		{
			name:           "modified path with leading slash",
			kubeletRootDir: "/var/lib/docker/kubelet/",
			expectVal:      "/var/lib/docker/kubelet/" + DefaultPluginMountPath + "/" + volumeID,
		},
		{
			name:           "modified path without leading slash",
			kubeletRootDir: "/var/lib/docker/kubelet",
			expectVal:      "/var/lib/docker/kubelet/" + DefaultPluginMountPath + "/" + volumeID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(KubeletRootDirEnvKey, tc.kubeletRootDir)
			d, _ := NewDriver("test-driver", "0.1", endpoint, "", true, "", "", false, 0, false, 0)
			expectVal := d.getDefaultMountPoint(volumeID)
			if expectVal != tc.expectVal {
				t.Fatalf("Got %s expected %s", expectVal, tc.expectVal)
			}
		})
	}

}

func TestNodeGetVolumeStats(t *testing.T) {
	endpoint := "unix://" + testsocket
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "2.0",
		endpoint:         endpoint,
		storageProviders: make(map[string]*cachedProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
	}

	tempDir, err := os.MkdirTemp("", "ngvs")
	if err != nil {
		t.Fatalf("Failed to set up temp dir: %v", err)
	}

	// setup the staging paths
	targetPath := filepath.Join(tempDir, defaultTargetPath)
	os.MkdirAll(targetPath, 0750)
	stagingPath := filepath.Join(tempDir, defaultStagingPath)
	os.MkdirAll(stagingPath, 0750)
	defer os.RemoveAll(targetPath)
	defer os.RemoveAll(stagingPath)
	defer os.RemoveAll(tempDir)

	testCases := []struct {
		name       string
		volumeID   string
		volumePath string
		expectErr  bool
	}{
		{
			name:       "normal",
			volumeID:   defaultVolumeID,
			volumePath: targetPath,
		},
		{
			name:       "no vol id",
			volumePath: targetPath,
			expectErr:  true,
		},
		{
			name:      "no vol path",
			volumeID:  defaultVolumeID,
			expectErr: true,
		},
		{
			name:       "bad vol path",
			volumeID:   defaultVolumeID,
			volumePath: "/mnt/fake",
			expectErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			req := &csi.NodeGetVolumeStatsRequest{
				VolumeId:   tc.volumeID,
				VolumePath: tc.volumePath,
			}
			_, err := driver.NodeGetVolumeStats(context.Background(), req)
			if err != nil && !tc.expectErr {
				t.Fatalf("Got unexpected err: %v", err)
			}
			if err == nil && tc.expectErr {
				t.Fatal("Did not get error but expected one")
			}
		})
	}
}

// TestReadStagedDeviceInfo exercises readStagedDeviceInfo, which reads the
// staged device info file. This validates the DRV-A4 fix that replaced the
// deprecated ioutil.ReadFile with os.ReadFile — the read path must still
// correctly load and unmarshal the device info JSON.
func TestReadStagedDeviceInfo(t *testing.T) {
	stagingDir := t.TempDir()

	want := &StagingDevice{
		VolumeID:         defaultVolumeID,
		VolumeAccessMode: model.MountType,
		Device: &model.Device{
			Pathname:     "dm-0",
			SerialNumber: "serial-123",
		},
		MountInfo: &Mount{
			MountPoint: "/var/lib/kubelet/staged",
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Failed to marshal staging device: %v", err)
	}
	if err := os.WriteFile(path.Join(stagingDir, deviceInfoFileName), data, 0600); err != nil {
		t.Fatalf("Failed to write device info file: %v", err)
	}

	got, err := readStagedDeviceInfo(stagingDir)
	if err != nil {
		t.Fatalf("readStagedDeviceInfo() returned unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("readStagedDeviceInfo() returned nil staging device")
	}
	if got.VolumeID != want.VolumeID {
		t.Errorf("VolumeID = %q, want %q", got.VolumeID, want.VolumeID)
	}
	if got.VolumeAccessMode != want.VolumeAccessMode {
		t.Errorf("VolumeAccessMode = %v, want %v", got.VolumeAccessMode, want.VolumeAccessMode)
	}
	if got.Device == nil || got.Device.Pathname != want.Device.Pathname {
		t.Errorf("Device.Pathname mismatch, got %+v, want %+v", got.Device, want.Device)
	}
	if got.MountInfo == nil || got.MountInfo.MountPoint != want.MountInfo.MountPoint {
		t.Errorf("MountInfo.MountPoint mismatch, got %+v, want %+v", got.MountInfo, want.MountInfo)
	}
}

// TestReadStagedDeviceInfo_FileNotExist verifies that readStagedDeviceInfo
// returns an error (and does not panic) when the device info file is absent.
func TestReadStagedDeviceInfo_FileNotExist(t *testing.T) {
	// Empty temp dir => deviceInfo.json does not exist.
	emptyDir := t.TempDir()

	got, err := readStagedDeviceInfo(emptyDir)
	if err == nil {
		t.Fatal("readStagedDeviceInfo() expected an error for missing file, got nil")
	}
	if got != nil {
		t.Errorf("readStagedDeviceInfo() expected nil device on error, got %+v", got)
	}
}

// TestReadStagedDeviceInfo_InvalidJSON verifies that readStagedDeviceInfo
// surfaces an unmarshalling error when the file content is not valid JSON.
// This confirms the os.ReadFile data is still passed through json.Unmarshal.
func TestReadStagedDeviceInfo_InvalidJSON(t *testing.T) {
	stagingDir := t.TempDir()

	if err := os.WriteFile(path.Join(stagingDir, deviceInfoFileName), []byte("{not-valid-json"), 0600); err != nil {
		t.Fatalf("Failed to write device info file: %v", err)
	}

	got, err := readStagedDeviceInfo(stagingDir)
	if err == nil {
		t.Fatal("readStagedDeviceInfo() expected an unmarshalling error, got nil")
	}
	if got != nil {
		t.Errorf("readStagedDeviceInfo() expected nil device on error, got %+v", got)
	}
}

// --- DRV-B2: Stage/Unstage per-volume locking tests ---
//
// These tests validate that the package-level stage/unstage locks are
// per-volume (concurrent.MapMutex) rather than a single global mutex, so that
// operations on different volumes can proceed concurrently while operations on
// the same volume remain serialized.

// TestStageLock_IsPerVolumeMapMutex is a compile-time/behavioral guard ensuring
// the stage/unstage locks are keyed MapMutexes (the DRV-B2 fix), not global
// sync.Mutex values. If someone reverts to a global mutex, this file no longer
// compiles because Lock/Unlock would not accept a key argument.
func TestStageLock_IsPerVolumeMapMutex(t *testing.T) {
	var _ *concurrent.MapMutex = stageLock
	var _ *concurrent.MapMutex = unstageLock
	var _ *concurrent.MapMutex = ephemeralPublishLock
	var _ *concurrent.MapMutex = ephemeralUnpublishLock

	// Locking and unlocking a key must not panic or deadlock.
	stageLock.Lock("vol-guard")
	stageLock.Unlock("vol-guard")
}

// TestStageLock_DifferentVolumesNotSerialized verifies that holding the stage
// lock for one volume does NOT block staging a different volume. Before the
// DRV-B2 fix, a single global mutex would have blocked here and the test would
// time out.
func TestStageLock_DifferentVolumesNotSerialized(t *testing.T) {
	const volA = "pvc-aaaaaaaa"
	const volB = "pvc-bbbbbbbb"

	// Hold the lock for volA for the duration of the test.
	stageLock.Lock(volA)
	defer stageLock.Unlock(volA)

	done := make(chan struct{})
	go func() {
		// A different volume must be lockable while volA is held.
		stageLock.Lock(volB)
		stageLock.Unlock(volB)
		close(done)
	}()

	select {
	case <-done:
		// Success: volB was not blocked by volA's lock.
	case <-time.After(2 * time.Second):
		t.Fatal("staging a different volume was blocked by another volume's lock (global mutex regression)")
	}
}

// TestStageLock_SameVolumeSerialized verifies that the per-volume lock still
// serializes concurrent operations on the SAME volume — i.e., we preserved the
// correctness guarantee while improving concurrency.
func TestStageLock_SameVolumeSerialized(t *testing.T) {
	const vol = "pvc-cccccccc"

	var concurrentHolders int32
	var maxObserved int32
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unstageLock.Lock(vol)
			// Track how many goroutines are inside the critical section.
			n := atomic.AddInt32(&concurrentHolders, 1)
			for {
				prev := atomic.LoadInt32(&maxObserved)
				if n <= prev || atomic.CompareAndSwapInt32(&maxObserved, prev, n) {
					break
				}
			}
			// Hold briefly to give other goroutines a chance to overlap if the
			// lock were not exclusive for this key.
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&concurrentHolders, -1)
			unstageLock.Unlock(vol)
		}()
	}
	wg.Wait()

	if maxObserved != 1 {
		t.Fatalf("expected same-volume operations to be serialized (max 1 holder), observed %d concurrent holders", maxObserved)
	}
}

// TestStageLock_ConcurrentDistinctVolumes ensures many distinct volumes can be
// locked and unlocked concurrently without deadlock, and that the MapMutex
// cleans up keys (no panic on repeated lock/unlock cycles).
func TestStageLock_ConcurrentDistinctVolumes(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "pvc-" + strconv.Itoa(id)
			stageLock.Lock(key)
			time.Sleep(time.Millisecond)
			stageLock.Unlock(key)
		}(i)
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// All distinct-volume locks completed without deadlock.
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent locking of distinct volumes deadlocked or hung")
	}
}

// --- DRV-B4: isVolumeStaged / nodeUnstageVolume must not silently ignore
// readStagedDeviceInfo errors ---
//
// Before the fix, both isVolumeStaged and nodeUnstageVolume used
// `stagingDev, _ := readStagedDeviceInfo(...)` and only checked for a nil
// device. A corrupted/partially-written deviceInfo.json (which still "exists")
// would therefore be treated as "not staged" / "already unstaged", potentially
// causing a duplicate device attachment or a leaked device. The fix surfaces
// the read/unmarshal error as codes.Internal.

// writeCorruptDeviceInfo writes an unparseable deviceInfo.json into a fresh
// staging directory and returns the directory path.
func writeCorruptDeviceInfo(t *testing.T) string {
	t.Helper()
	stagingDir := t.TempDir()
	if err := os.WriteFile(path.Join(stagingDir, deviceInfoFileName), []byte("{not-valid-json"), 0600); err != nil {
		t.Fatalf("Failed to write corrupt device info file: %v", err)
	}
	return stagingDir
}

// TestIsVolumeStaged_CorruptDeviceInfoReturnsError verifies that a corrupted
// device info file causes isVolumeStaged to return an error (codes.Internal)
// rather than silently reporting the volume as "not staged" (DRV-B4).
func TestIsVolumeStaged_CorruptDeviceInfoReturnsError(t *testing.T) {
	driver := &Driver{}
	stagingDir := writeCorruptDeviceInfo(t)

	staged, err := driver.isVolumeStaged(
		defaultVolumeID,
		stagingDir,
		"",
		model.MountType,
		nil, // volumeCapability (unused on this path)
		nil, // secrets
		nil, // publishContext
		nil, // volumeContext
	)

	if err == nil {
		t.Fatal("isVolumeStaged() expected an error for corrupt device info, got nil")
	}
	if staged {
		t.Error("isVolumeStaged() must report not-staged (false) when the device info is unreadable")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("isVolumeStaged() error code = %v, want %v", status.Code(err), codes.Internal)
	}
}

// TestIsVolumeStaged_NoFileReturnsNotStaged verifies the benign case still
// behaves correctly: when no device info file exists, isVolumeStaged returns
// (false, nil) without error.
func TestIsVolumeStaged_NoFileReturnsNotStaged(t *testing.T) {
	driver := &Driver{}
	emptyDir := t.TempDir() // no deviceInfo.json

	staged, err := driver.isVolumeStaged(
		defaultVolumeID,
		emptyDir,
		"",
		model.MountType,
		nil,
		nil,
		nil,
		nil,
	)

	if err != nil {
		t.Fatalf("isVolumeStaged() unexpected error for missing file: %v", err)
	}
	if staged {
		t.Error("isVolumeStaged() should report not-staged (false) when no device info file exists")
	}
}

// TestNodeUnstageVolume_CorruptDeviceInfoReturnsError verifies that a corrupted
// device info file causes nodeUnstageVolume to return an error (codes.Internal)
// rather than silently reporting the volume as already unstaged, which would
// leak the underlying device (DRV-B4).
func TestNodeUnstageVolume_CorruptDeviceInfoReturnsError(t *testing.T) {
	driver := &Driver{}
	stagingDir := writeCorruptDeviceInfo(t)

	err := driver.nodeUnstageVolume(defaultVolumeID, stagingDir)
	if err == nil {
		t.Fatal("nodeUnstageVolume() expected an error for corrupt device info, got nil")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("nodeUnstageVolume() error code = %v, want %v", status.Code(err), codes.Internal)
	}
}

// TestNodeUnstageVolume_NoFileReturnsSuccess verifies the benign idempotent
// case: when no device info file exists, nodeUnstageVolume returns nil (already
// unstaged) without error.
func TestNodeUnstageVolume_NoFileReturnsSuccess(t *testing.T) {
	driver := &Driver{}
	emptyDir := t.TempDir() // no deviceInfo.json

	if err := driver.nodeUnstageVolume(defaultVolumeID, emptyDir); err != nil {
		t.Fatalf("nodeUnstageVolume() unexpected error for missing file (idempotent unstage): %v", err)
	}
}

// --- DRV-B5: stale device cleanup failure must block staging ---
//
// Before the fix, setupDevice only logged a warning when DeleteDevice failed
// for a stale device and then proceeded to CreateDevices, risking a failed
// create or a bind to the wrong multipath path. The fix routes stale-device
// cleanup through deleteStaleDevice, which retries a bounded number of times
// and returns codes.Internal when cleanup ultimately fails.

// staleDeviceFakeDriver embeds the vendored chapi.FakeDriver and overrides
// DeleteDevice so tests can control how many times the delete fails before it
// (optionally) succeeds.
type staleDeviceFakeDriver struct {
	*chapi.FakeDriver
	mu             sync.Mutex
	deleteCalls    int
	failuresToEmit int   // number of leading DeleteDevice calls that return an error
	deleteErr      error // error returned while failing
}

func (d *staleDeviceFakeDriver) DeleteDevice(device *model.Device) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleteCalls++
	if d.deleteCalls <= d.failuresToEmit {
		return d.deleteErr
	}
	return nil
}

func (d *staleDeviceFakeDriver) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deleteCalls
}

// newStaleDeviceTestDriver returns a *Driver wired with the supplied chapi
// driver, suitable for exercising deleteStaleDevice.
func newStaleDeviceTestDriver(chapiDrv chapi.Driver) *Driver {
	return &Driver{
		name:             "fake-test-driver",
		version:          "2.0",
		storageProviders: make(map[string]*cachedProvider),
		chapiDriver:      chapiDrv,
		flavor:           &vanilla.Flavor{},
	}
}

// TestDeleteStaleDevice_PersistentFailureReturnsError verifies that when
// DeleteDevice always fails, deleteStaleDevice exhausts its retries and returns
// a codes.Internal error (DRV-B5) instead of silently succeeding.
func TestDeleteStaleDevice_PersistentFailureReturnsError(t *testing.T) {
	fake := &staleDeviceFakeDriver{
		FakeDriver:     &chapi.FakeDriver{},
		failuresToEmit: staleDeviceCleanupMaxAttempts, // fail every attempt
		deleteErr:      errors.New("device busy"),
	}
	driver := newStaleDeviceTestDriver(fake)

	device := &model.Device{AltFullPathName: "/dev/mapper/stale"}
	err := driver.deleteStaleDevice(defaultVolumeID, device)

	if err == nil {
		t.Fatal("deleteStaleDevice() expected an error when cleanup always fails, got nil")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("deleteStaleDevice() error code = %v, want %v", status.Code(err), codes.Internal)
	}
	if got := fake.callCount(); got != staleDeviceCleanupMaxAttempts {
		t.Errorf("DeleteDevice call count = %d, want %d (all attempts exhausted)", got, staleDeviceCleanupMaxAttempts)
	}
}

// TestDeleteStaleDevice_SucceedsAfterRetry verifies the transient-failure case:
// the first attempt fails but a subsequent retry succeeds, so deleteStaleDevice
// returns nil and does not block staging.
func TestDeleteStaleDevice_SucceedsAfterRetry(t *testing.T) {
	if staleDeviceCleanupMaxAttempts < 2 {
		t.Skip("retry behavior requires at least 2 attempts")
	}
	fake := &staleDeviceFakeDriver{
		FakeDriver:     &chapi.FakeDriver{},
		failuresToEmit: 1, // fail once, then succeed
		deleteErr:      errors.New("device transiently busy"),
	}
	driver := newStaleDeviceTestDriver(fake)

	device := &model.Device{AltFullPathName: "/dev/mapper/stale"}
	if err := driver.deleteStaleDevice(defaultVolumeID, device); err != nil {
		t.Fatalf("deleteStaleDevice() expected success after retry, got err: %v", err)
	}
	if got := fake.callCount(); got != 2 {
		t.Errorf("DeleteDevice call count = %d, want 2 (one failure + one success)", got)
	}
}

// TestDeleteStaleDevice_SucceedsFirstTry verifies the happy path: cleanup
// succeeds on the first attempt with no retries.
func TestDeleteStaleDevice_SucceedsFirstTry(t *testing.T) {
	fake := &staleDeviceFakeDriver{
		FakeDriver:     &chapi.FakeDriver{},
		failuresToEmit: 0, // never fail
	}
	driver := newStaleDeviceTestDriver(fake)

	device := &model.Device{AltFullPathName: "/dev/mapper/stale"}
	if err := driver.deleteStaleDevice(defaultVolumeID, device); err != nil {
		t.Fatalf("deleteStaleDevice() unexpected error on first-try success: %v", err)
	}
	if got := fake.callCount(); got != 1 {
		t.Errorf("DeleteDevice call count = %d, want 1 (single successful attempt)", got)
	}
}

// TestSetupDevice_StaleCleanupFailureBlocksStaging verifies that setupDevice
// surfaces the stale-device cleanup failure (DRV-B5): when GetDevice reports a
// stale device and DeleteDevice keeps failing, setupDevice must return an error
// rather than proceeding to CreateDevices.
func TestSetupDevice_StaleCleanupFailureBlocksStaging(t *testing.T) {
	fake := &staleDeviceFakeDriver{
		FakeDriver:     &chapi.FakeDriver{}, // GetDevice returns a non-nil stale device
		failuresToEmit: staleDeviceCleanupMaxAttempts,
		deleteErr:      errors.New("device busy"),
	}
	driver := newStaleDeviceTestDriver(fake)

	publishContext := map[string]string{
		accessProtocolKey: iscsi,
		targetNamesKey:    "iqn.2003-10.com.3pardata:fake",
		serialNumberKey:   "fakeSerial",
	}

	dev, err := driver.setupDevice(defaultVolumeID, map[string]string{}, publishContext, map[string]string{})
	if err == nil {
		t.Fatal("setupDevice() expected an error when stale device cleanup fails, got nil")
	}
	if dev != nil {
		t.Errorf("setupDevice() expected nil device on cleanup failure, got %+v", dev)
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("setupDevice() error code = %v, want %v", status.Code(err), codes.Internal)
	}
}

// TestGetEphemeralVolName_NoCollisionOnTruncation is a regression test for
// DRV-D7. The pod-name infix is truncated to the last 32 characters, so two
// pods whose names share the same trailing 32 characters and use the same
// volume handle previously produced an identical ephemeral volume name. The fix
// appends a hash of the full pod name and volume handle to guarantee unique
// names. This test asserts that:
//  1. The generated name carries the ephemeral prefix and volume handle.
//  2. Two pods that collide under truncation get distinct names.
//  3. The same inputs are deterministic (stable name across calls).
//  4. Different volume handles for the same pod produce distinct names.
func TestGetEphemeralVolName_NoCollisionOnTruncation(t *testing.T) {
	const volumeHandle = "pvc-1234567890"

	// Two distinct pod names that share the same trailing 32 characters. Under
	// pure truncation these collapse to the same infix.
	commonSuffix := "deployment-pod-abcdefghijklmnop" // 31 chars of shared tail
	podA := "alpha-" + commonSuffix
	podB := "bravo-" + commonSuffix

	nameA := getEphemeralVolName(podA, volumeHandle)
	nameB := getEphemeralVolName(podB, volumeHandle)

	// Sanity: both names carry the ephemeral prefix and the volume handle.
	if !strings.HasPrefix(nameA, ephemeralKey+"-") {
		t.Errorf("name %q missing ephemeral prefix %q", nameA, ephemeralKey)
	}
	if !strings.Contains(nameA, volumeHandle) {
		t.Errorf("name %q does not contain volume handle %q", nameA, volumeHandle)
	}

	// Core DRV-D7 assertion: colliding truncated infixes must NOT collide.
	if nameA == nameB {
		t.Errorf("ephemeral volume names collided for distinct pods: %q == %q", nameA, nameB)
	}

	// Determinism: identical inputs yield identical names.
	if got := getEphemeralVolName(podA, volumeHandle); got != nameA {
		t.Errorf("getEphemeralVolName not deterministic: %q != %q", got, nameA)
	}

	// Different volume handles for the same pod must also differ.
	if same := getEphemeralVolName(podA, "pvc-9999999999"); same == nameA {
		t.Errorf("expected distinct names for different volume handles, both were %q", same)
	}

	// Short pod names (no truncation) should still be unique per pod.
	shortA := getEphemeralVolName("p1", volumeHandle)
	shortB := getEphemeralVolName("p2", volumeHandle)
	if shortA == shortB {
		t.Errorf("short pod names collided: %q == %q", shortA, shortB)
	}
}
func TestValidateStorageClassBoolParam(t *testing.T) {
	endpoint := "unix://" + testsocket
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "2.0",
		endpoint:         endpoint,
		storageProviders: make(map[string]*cachedProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
	}

	testCases := []struct {
		name      string
		params    map[string]string
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid true",
			params:    map[string]string{"fsRepair": "true"},
			expectErr: false,
		},
		{
			name:      "valid false",
			params:    map[string]string{"fsRepair": "false"},
			expectErr: false,
		},
		{
			name:      "not set (empty map)",
			params:    map[string]string{},
			expectErr: false,
		},
		{
			name:      "empty string value",
			params:    map[string]string{"fsRepair": ""},
			expectErr: false,
		},
		{
			name:      "invalid - uppercase True",
			params:    map[string]string{"fsRepair": "True"},
			expectErr: true,
			errMsg:    `invalid value "True" for the fsRepair parameter`,
		},
		{
			name:      "invalid - uppercase TRUE",
			params:    map[string]string{"fsRepair": "TRUE"},
			expectErr: true,
			errMsg:    `invalid value "TRUE" for the fsRepair parameter`,
		},
		{
			name:      "invalid - uppercase FALSE",
			params:    map[string]string{"fsRepair": "FALSE"},
			expectErr: true,
			errMsg:    `invalid value "FALSE" for the fsRepair parameter`,
		},
		{
			name:      "invalid - typo Tru",
			params:    map[string]string{"fsRepair": "Tru"},
			expectErr: true,
			errMsg:    `invalid value "Tru" for the fsRepair parameter`,
		},
		{
			name:      "invalid - arbitrary string enableit",
			params:    map[string]string{"fsRepair": "enableit"},
			expectErr: true,
			errMsg:    `invalid value "enableit" for the fsRepair parameter`,
		},
		{
			name:      "invalid - yes",
			params:    map[string]string{"fsRepair": "yes"},
			expectErr: true,
			errMsg:    `invalid value "yes" for the fsRepair parameter`,
		},
		{
			name:      "invalid - 1",
			params:    map[string]string{"fsRepair": "1"},
			expectErr: true,
			errMsg:    `invalid value "1" for the fsRepair parameter`,
		},
		{
			name:      "valid true with other params",
			params:    map[string]string{"fsRepair": "true", "cpg": "SSD_r6", "accessProtocol": "iscsi"},
			expectErr: false,
		},
		{
			name:      "invalid value with other params",
			params:    map[string]string{"fsRepair": "Enable", "cpg": "SSD_r6", "accessProtocol": "iscsi"},
			expectErr: true,
			errMsg:    `invalid value "Enable" for the fsRepair parameter`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := driver.validateStorageClassBoolParam(tc.params, "fsRepair")
			if err != nil && !tc.expectErr {
				t.Fatalf("Got unexpected error: %v", err)
			}
			if err == nil && tc.expectErr {
				t.Fatal("Expected error but got none")
			}
			if err != nil && tc.expectErr && tc.errMsg != "" {
				if !strings.Contains(err.Error(), tc.errMsg) {
					t.Fatalf("Error message %q does not contain expected %q", err.Error(), tc.errMsg)
				}
			}
		})
	}
}

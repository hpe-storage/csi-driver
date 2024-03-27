// Copyright 2020 Hewlett Packard Enterprise Development LP
package driver

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/concurrent"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
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
		name:              "fake-test-driver",
		version:           "2.0",
		endpoint:          endpoint,
		storageProviders:  make(map[string]storageprovider.StorageProvider),
		chapiDriver:       &chapi.FakeDriver{},
		flavor:            &vanilla.Flavor{},
		requestCache:      make(map[string]interface{}),
		requestCacheMutex: concurrent.NewMapMutex(),
	}

	tempDir, err := ioutil.TempDir("", "ngvs")
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

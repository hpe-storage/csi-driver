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
		name:             "fake-test-driver",
		version:          "2.0",
		endpoint:         endpoint,
		storageProviders: make(map[string]storageprovider.StorageProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
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

func TestValidateFsRepairParameter(t *testing.T) {
	endpoint := "unix://" + testsocket
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "2.0",
		endpoint:         endpoint,
		storageProviders: make(map[string]storageprovider.StorageProvider),
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
			err := driver.validateFsRepairParameter(tc.params)
			if err != nil && !tc.expectErr {
				t.Fatalf("Got unexpected error: %v", err)
			}
			if err == nil && tc.expectErr {
				t.Fatal("Expected error but got none")
			}
			if err != nil && tc.expectErr && tc.errMsg != "" {
				if !contains(err.Error(), tc.errMsg) {
					t.Fatalf("Error message %q does not contain expected %q", err.Error(), tc.errMsg)
				}
			}
		})
	}
}

func TestNodeStageVolumeFsRepairValidation(t *testing.T) {
	endpoint := "unix://" + testsocket
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "2.0",
		endpoint:         endpoint,
		storageProviders: make(map[string]storageprovider.StorageProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
	}
	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	})

	testCases := []struct {
		name          string
		volumeContext map[string]string
		expectErr     bool
		errSubstring  string
	}{
		{
			name:          "valid fsRepair true - passes validation",
			volumeContext: map[string]string{"fsRepair": "true", "accessProtocol": "iscsi"},
			expectErr:     false,
		},
		{
			name:          "valid fsRepair false - passes validation",
			volumeContext: map[string]string{"fsRepair": "false", "accessProtocol": "iscsi"},
			expectErr:     false,
		},
		{
			name:          "no fsRepair - passes validation",
			volumeContext: map[string]string{"accessProtocol": "iscsi"},
			expectErr:     false,
		},
		{
			name:          "invalid fsRepair Tru - rejected",
			volumeContext: map[string]string{"fsRepair": "Tru", "accessProtocol": "iscsi"},
			expectErr:     true,
			errSubstring:  `invalid value "Tru" for fsRepair parameter`,
		},
		{
			name:          "invalid fsRepair enableit - rejected",
			volumeContext: map[string]string{"fsRepair": "enableit", "accessProtocol": "iscsi"},
			expectErr:     true,
			errSubstring:  `invalid value "enableit" for fsRepair parameter`,
		},
		{
			name:          "invalid fsRepair True (capital) - rejected",
			volumeContext: map[string]string{"fsRepair": "True", "accessProtocol": "iscsi"},
			expectErr:     true,
			errSubstring:  `invalid value "True" for fsRepair parameter`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &csi.NodeStageVolumeRequest{
				VolumeId:          defaultVolumeID,
				StagingTargetPath: "/tmp/staging-test",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							FsType: "ext4",
						},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
				VolumeContext: tc.volumeContext,
			}

			_, err := driver.NodeStageVolume(context.Background(), req)
			if tc.expectErr {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				if tc.errSubstring != "" && !contains(err.Error(), tc.errSubstring) {
					t.Fatalf("Error %q does not contain expected substring %q", err.Error(), tc.errSubstring)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

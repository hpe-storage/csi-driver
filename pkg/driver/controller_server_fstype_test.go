// Copyright 2026 Hewlett Packard Enterprise Development LP
package driver

import (
	"errors"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/fake"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
)

// TestRequestedFsTypeConflictsWithParent validates the ESC-18117 fix: when restoring a
// PVC from a VolumeSnapshot, the fsType-parity guard must only fire between two filesystem
// (Mount) volumes. A raw Block target has an empty requested filesystem and must never be
// rejected, so a Filesystem->Block volumeMode conversion (KEP-3141) is allowed.
func TestRequestedFsTypeConflictsWithParent(t *testing.T) {
	tests := []struct {
		name                string
		parentVolFsType     string
		requestedFilesystem string
		wantConflict        bool
	}{
		{
			name:                "ESC-18117: filesystem parent, block target (empty fs) is allowed",
			parentVolFsType:     "xfs",
			requestedFilesystem: "",
			wantConflict:        false,
		},
		{
			name:                "block parent, block target is allowed",
			parentVolFsType:     "",
			requestedFilesystem: "",
			wantConflict:        false,
		},
		{
			name:                "block parent, filesystem target is allowed",
			parentVolFsType:     "",
			requestedFilesystem: "xfs",
			wantConflict:        false,
		},
		{
			name:                "same filesystem on both is allowed",
			parentVolFsType:     "xfs",
			requestedFilesystem: "xfs",
			wantConflict:        false,
		},
		{
			name:                "different filesystem is a conflict",
			parentVolFsType:     "xfs",
			requestedFilesystem: "ext4",
			wantConflict:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestedFsTypeConflictsWithParent(tt.parentVolFsType, tt.requestedFilesystem)
			assert.Equal(t, tt.wantConflict, got)
		})
	}
}

// stubRestoreFlavor embeds the vanilla flavor and lets a test control the result
// of GetVolumePropertyOfPV, the parent-PV fsType lookup used by the snapshot-restore
// path of createVolume. The vanilla flavor never returns an error, so a stub is
// required to exercise the lookup-failure branch.
type stubRestoreFlavor struct {
	*vanilla.Flavor
	fsType string
	err    error
}

func (f *stubRestoreFlavor) GetVolumePropertyOfPV(propertyName, pvName string) (string, error) {
	return f.fsType, f.err
}

func newRestoreTestDriver(fl *stubRestoreFlavor) (*Driver, storageprovider.StorageProvider) {
	d := &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		storageProviders: make(map[string]storageprovider.StorageProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           fl,
	}
	d.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
	})
	d.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	})

	fsp := fake.NewFakeStorageProvider()
	cred := &storageprovider.Credentials{Username: "fake", Backend: "fake", ServiceName: "fake"}
	d.storageProviders[d.GenerateStorageProviderCacheKey(cred)] = fsp
	return d, fsp
}

// TestCreateVolumeFromSnapshotParentFsTypeLookup covers the CON-4977 / issue #580 fix:
// when the snapshot's parent volume PV cannot be looked up in the local cluster
// (e.g. a cross-cluster restore from a snapshot on a shared array), the filesystem
// check must be skipped and the restore must proceed rather than failing CreateVolume.
// A successful lookup must still enforce the fsType match (no regression).
func TestCreateVolumeFromSnapshotParentFsTypeLookup(t *testing.T) {
	const (
		parentName = "pvc-parent"
		snapID     = "snapshot-1"
		cloneName  = "pvc-clone"
		size       = int64(16 * 1024 * 1024 * 1024)
	)
	secrets := map[string]string{
		"backend":     "fake",
		"username":    "fake",
		"password":    "fake",
		"serviceName": "fake",
		"servicePort": "8080",
	}
	snapSource := &csi.VolumeContentSource{
		Type: &csi.VolumeContentSource_Snapshot{
			Snapshot: &csi.VolumeContentSource_SnapshotSource{SnapshotId: snapID},
		},
	}

	tests := []struct {
		name         string
		lookupFsType string
		lookupErr    error
		requestedFs  string
		wantErr      bool
		wantCode     codes.Code
	}{
		{
			name:        "parent PV missing in this cluster proceeds (cross-cluster restore, #580)",
			lookupErr:   errors.New(`persistentvolumes "pvc-parent" not found`),
			requestedFs: "xfs",
			wantErr:     false,
		},
		{
			name:         "parent fsType matches requested proceeds",
			lookupFsType: "xfs",
			requestedFs:  "xfs",
			wantErr:      false,
		},
		{
			name:         "parent fsType mismatch is rejected",
			lookupFsType: "xfs",
			requestedFs:  "ext4",
			wantErr:      true,
			wantCode:     codes.InvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, fsp := newRestoreTestDriver(&stubRestoreFlavor{
				Flavor: &vanilla.Flavor{},
				fsType: tc.lookupFsType,
				err:    tc.lookupErr,
			})
			if _, err := fsp.CreateVolume(parentName, "", size, nil); err != nil {
				t.Fatalf("seed parent volume: %v", err)
			}
			if _, err := fsp.CreateSnapshot(snapID, "", parentName, nil); err != nil {
				t.Fatalf("seed snapshot: %v", err)
			}

			volCaps := []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{FsType: tc.requestedFs}},
					AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
				},
			}

			vol, err := d.createVolume(cloneName, size, volCaps, secrets, snapSource, nil, map[string]string{})

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (vol=%+v)", vol)
				}
				if status.Code(err) != tc.wantCode {
					t.Fatalf("expected code %v, got %v: %v", tc.wantCode, status.Code(err), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if vol == nil || vol.VolumeId != cloneName {
				t.Fatalf("expected cloned volume %q, got %+v", cloneName, vol)
			}
		})
	}
}

// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

import (
	"github.com/hpe-storage/common-host-libs/model"
)

// StagingDevice represents the device information that is stored in the staging area.
type StagingDevice struct {
	VolumeID         string                 `json:"volume_id"`
	VolumeAccessMode model.VolumeAccessType `json:"volume_access_mode"` // block or mount
	Device           *model.Device          `json:"device"`
	MountInfo        *Mount                 `json:"mount_info,omitempty"`
}

// Mount :
type Mount struct {
	MountPoint        string                `json:"mount_point"` // Mounted directory
	MountOptions      []string              `json:"mount_options,omitempty"`
	FilesystemOptions *model.FilesystemOpts `json:"filesystem_options,omitempty"`
}

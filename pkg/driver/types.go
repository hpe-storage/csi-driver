// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

import (
	"github.com/hpe-storage/common-host-libs/model"
)

// StagingDevice represents the device information that is stored in the staging area.
type StagingDevice struct {
	VolumeID         string                 `json:"volume_id"`
	VolumeAccessMode model.VolumeAccessType `json:"volume_access_mode"` // block or mount
	POD              *POD                   `json:"pod,omitempty"`      // ephemeral inline volume
	Secret           *Secret                `json:"secret,omitempty"`   // secret for ephemeral inline volume
	Device           *model.Device          `json:"device"`
	MountInfo        *Mount                 `json:"mount_info,omitempty"`
}

// Mount :
type Mount struct {
	MountPoint        string                `json:"mount_point"` // Mounted directory
	MountOptions      []string              `json:"mount_options,omitempty"`
	FilesystemOptions *model.FilesystemOpts `json:"filesystem_options,omitempty"`
}

// POD represents the pod information of ephemeral inline volumes that is stored in the publish area
type POD struct {
	UID       string `json:"pod_uid"`
	Name      string `json:"pod_name"`
	Namespace string `json:"pod_namespace"`
}

// Secret represents the secret information of ephemeral inline volumes (Provided via volume attributes)
type Secret struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

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

// Ephemeral represents the ephemeral inline volume and its associated pod details.
type Ephemeral struct {
	VolumeID     string  `json:"volume_id"`            // Volume ID
	VolumeHandle string  `json:"volume_handle"`        // ephemeral volume handle
	PodData      *POD    `json:"pod_data,omitempty"`   // POD info of ephemeral volume
	SecretRef    *Secret `json:"secret_ref,omitempty"` // secret reference for ephemeral volume
}

// POD represents the pod information of ephemeral inline volume that is stored in the publish area
type POD struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Secret represents the secret information of ephemeral inline volumes (Provided via volume attributes)
type Secret struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// VolumeHandleTargetPath represents ephemeral volume handle and its assocatiate node stage/publish target path
type VolumeHandleTargetPath struct {
	VolumeHandle string `json:"volume_handle"` // ephemeral volume handle
	TargetPath   string `json:"target_path"`   // target path of ephemeral volume
}

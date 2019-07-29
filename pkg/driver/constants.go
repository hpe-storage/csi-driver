// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

const (
	// iSCSI protocol type
	iscsi = "iscsi"

	// defaultFileSystem is the implemenation-specific default value
	defaultFileSystem = "xfs"

	// Unsupported filesystem
	nfsFileSystem = "nfs"

	// Default mount directory
	defaultMountDir = "/opt/hpe-storage-mounts"

	// defaultVolumeSize is the implementation-specific default value in bytes
	defaultVolumeSize = 10 * 1024 * 1024 * 1024 // 10 GiB

	// MountFlagsSizeMaxAllowed is the CSI spec defined limit
	mountFlagsSizeMaxAllowed = 4 * 1024 // 4 KiB

	// Filesystem Details
	filesystemType  = "fsType"
	filesystemOwner = "fsOwner"
	filesystemMode  = "fsMode"

	// Volume Publish params
	serialNumber     = "serialNumber"
	accessProtocol   = "accessProtocol"
	targetName       = "targetName"
	targetScope      = "targetScope"
	lunID            = "lunId"
	discoveryIP      = "discoveryIp"
	chapUsername     = "chapUsername"
	chapPassword     = "chapPassword"
	readOnly         = "readOnly"
	volumeAccessMode = "volumeAccessMode"

	// deviceInfoFileName is used to store the device details in a JSON file
	deviceInfoFileName = "deviceInfo.json"

	nanos = 1000000000

	// Pending :
	Pending = "PENDING"
)

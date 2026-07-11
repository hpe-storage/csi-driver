// Copyright 2019,2025 Hewlett Packard Enterprise Development LP

package driver

import "time"

const (
	// Protocol types
	iscsi           = "iscsi"
	fc              = "fc"
	nvmeotcp        = "nvmeotcp"
	defaultNvmePort = "4420"
	nvmetcp         = "nvmetcp"

	// defaultFileSystem is the implemenation-specific default value
	defaultFileSystem = "xfs"

	// Default Kubelet Root directory
	DefaultKubeletRoot     string = "/var/lib/kubelet/"
	DefaultPluginMountPath string = "plugins/hpe.com/mounts"

	// Unsupported filesystem
	nfsFileSystem = "nfs"

	// GiB in bytes
	giB int64 = 1 * 1024 * 1024 * 1024

	// defaultVolumeSize is the implementation-specific default value in bytes
	defaultVolumeSize int64 = 10 * giB // 10 GiB in Bytes

	// MountFlagsSizeMaxAllowed is the CSI spec defined limit
	mountFlagsSizeMaxAllowed = 4 * 1024 // 4 KiB

	// Filesystem Details
	fsTypeKey          = "fsType"
	fsOwnerKey         = "fsOwner"
	fsModeKey          = "fsMode"
	fsCreateOptionsKey = "fsCreateOptions"

	// Group Target Scope
	targetScopeGroup = "group"

	// Volume Publish params
	serialNumberKey   = "serialNumber"
	accessProtocolKey = "accessProtocol"
	targetNamesKey    = "targetNames"
	targetScopeKey    = "targetScope"
	lunIDKey          = "lunId"

	secondaryArrayDetailsKey = "secondaryArrayDetailsKey"

	discoveryIPsKey       = "discoveryIps"
	readOnlyKey           = "readOnly"
	multiInitiatorKey     = "multiInitiator"
	defaultConnectionMode = "manual"
	KubeletRootDirEnvKey  = "KUBELET_ROOT_DIR"

	// Replication (Remote Copy) StorageClass parameter keys. These are the
	// camelCase keys as they appear in a StorageClass/PVC before being converted
	// to snake_case for the CSP. They are used to detect when array-based
	// replication is requested so that unsupported protocol combinations (e.g.
	// NVMe/TCP + replication, CROSS-2) can be rejected early in the driver.
	remoteCopyGroupKey                    = "remoteCopyGroup"
	replicationDevicesKey                 = "replicationDevices"
	oneRcgPerPvcKey                       = "oneRcgPerPvc"
	allowBatchReplicatedVolumeCreationKey = "allowBatchReplicatedVolumeCreation"

	// staleDeviceCleanupMaxAttempts bounds how many times stale-device cleanup
	// is attempted before staging is failed (DRV-B5). The first attempt plus
	// (max-1) retries gives transient failures a chance to clear without
	// blocking indefinitely.
	staleDeviceCleanupMaxAttempts = 2

	// staleDeviceCleanupRetryDelay is the delay between stale-device cleanup
	// attempts (DRV-B5).
	staleDeviceCleanupRetryDelay = 1 * time.Second

	// disable the node get volumestats
	disableNodeGetVolumeStatsKey = "DISABLE_NODE_GET_VOLUMESTATS"

	// deviceInfoFileName is used to store the device details in a JSON file
	deviceInfoFileName = "deviceInfo.json"

	// volDataFileName stores the volume data
	volDataFileName = "vol_data.json"

	// ephemeralDataFileName stores Volume ID, POD info and Secrets reference
	ephemeralDataFileName = "ephemeral_data.json"

	// driverModeKey stored in the vol_data.json file
	driverModeKey = "driverMode"

	// volumeLifecycleMode stored in the vol_data.json file
	volumeLifecycleModeKey = "volumeLifecycleMode"

	// ephemeralVolumeMode for ephemeral inline volume
	ephemeralVolumeMode = "ephemeral"

	// Default scrubber interval for Ephemeral inline volumes on the node.
	defaultInlineVolumeScrubberInterval = 3600 // Default value is 3600 seconds or 1 Hour
	inlineVolumeScrubberIntervalKey     = "INLINE_VOLUMES_SCRUBBER_INTERVAL"

	// Pending :
	Pending = "PENDING"

	// common properties
	descriptionKey        = "description"
	protectionTemplateKey = "protectionTemplate"

	// csiEphemeral attribute to be used to specify ephemeral inline volume request type
	csiEphemeral = "csi.storage.k8s.io/ephemeral"

	// secretNamespace attribute to be used to specify secret namespace name for ephemeral inline volume
	secretNamespaceKey = "inline-volume-secret-namespace"

	// secretName attribute to be used to specify secret name for ephemeral inline volume
	secretNameKey = "inline-volume-secret-name"

	hostEncryptionKey                = "hostEncryption"
	hostEncryptionSecretNameKey      = "hostEncryptionSecretName"
	hostEncryptionSecretNamespaceKey = "hostEncryptionSecretNamespace"
	hostEncryptionPassphraseKey      = "hostEncryptionPassphrase"

	// POD attributes propogated to the CSI
	csiEphemeralPodName      = "csi.storage.k8s.io/pod.name"
	csiEphemeralPodNamespace = "csi.storage.k8s.io/pod.namespace"
	csiEphemeralPodUID       = "csi.storage.k8s.io/pod.uid"

	// Prefix used by CSI ephemeral inline volume names
	ephemeralKey = "ephemeral"

	// Ephemeral volume size specified in the POD spec
	sizeKey = "size"

	trueKey  = "true"
	falseKey = "false"
	// nfs properties
	nfsResourcesKey = "nfsResources"
	// indicates if this is an underlying NFS PVC(not exposed to user)
	nfsPVCKey             = "nfsPVC"
	nfsMountOptionsKey    = "nfsMountOptions"
	nfsNamespaceKey       = "nfsNamespace"
	nfsSourceNamespaceKey = "csi.storage.k8s.io/pvc/namespace"
	defaultNFSNamespace   = "hpe-nfs"

	// Maximum default number of volumes that controller can publish to the node.
	defaultMaxVolPerNode = 100
	maxVolumesPerNodeKey = "MAX_VOLUMES_PER_NODE"

	defaultCSITopologyKey = "csi.hpe.com/zone"
	// FileSystem Corruption parameters
	fsRepairKey = "fsRepair"

	// File volume constants
	fileExportIPKey                = "exportIP"
	mountPathKey                   = "mountPath"
	fileVolumeNameKey              = "csi.storage.k8s.io/pv/name"
	accessControlListKey           = "accessControlList"
	accessControlListAnnotationKey = "csi.hpe.com/accessControlList"
)

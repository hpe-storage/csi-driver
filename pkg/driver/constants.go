// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

const (
	// Protocol types
	iscsi = "iscsi"
	fc    = "fc"

	// defaultFileSystem is the implemenation-specific default value
	defaultFileSystem = "xfs"

	// Unsupported filesystem
	nfsFileSystem = "nfs"

	// Default mount directory
	defaultMountDir = "/var/lib/kubelet/plugins/hpe.com/mounts"

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
	serialNumberKey       = "serialNumber"
	accessProtocolKey     = "accessProtocol"
	targetNamesKey        = "targetNames"
	targetScopeKey        = "targetScope"
	lunIDKey              = "lunId"
	discoveryIPsKey       = "discoveryIps"
	chapUsernameKey       = "chapUsername"
	chapPasswordKey       = "chapPassword"
	readOnlyKey           = "readOnly"
	volumeAccessModeKey   = "volumeAccessMode"
	multiInitiatorKey     = "multiInitiator"
	defaultConnectionMode = "manual"

	// deviceInfoFileName is used to store the device details in a JSON file
	deviceInfoFileName = "deviceInfo.json"

	// Pending :
	Pending = "PENDING"

	// common properties
	descriptionKey = "description"

	// csiEphemeral attribute to be used to specify ephemeral inline volume request type
	csiEphemeral = "csi.storage.k8s.io/ephemeral"

	// secretNamespace attribute to be used to specify secret namespace name for ephemeral inline volume
	secretNamespaceKey = "inline-volume-secret-namespace"

	// secretName attribute to be used to specify secret name for ephemeral inline volume
	secretNameKey = "inline-volume-secret-name"

	// POD attributes propogated to the CSI
	csiEphemeralPodName      = "csi.storage.k8s.io/pod.name"
	csiEphemeralPodNamespace = "csi.storage.k8s.io/pod.namespace"
	csiEphemeralPodUID       = "csi.storage.k8s.io/pod.uid"

	// Prefix used by CSI ephemeral inline volume names
	ephemeralKey = "ephemeral"

	// Ephemeral volume size specified in the POD spec
	sizeKey = "size"

	trueKey = "true"

	// nfs properties
	nfsResourcesKey = "nfsResources"
	// indicates if this is an underlying NFS PVC(not exposed to user)
	nfsPVCKey = "nfsPVC"

	// Maximum default number of volumes that controller can publish to the node.
	defaultMaxVolPerNode = 100
	maxVolumesPerNodeKey = "MAX_VOLUMES_PER_NODE"
)

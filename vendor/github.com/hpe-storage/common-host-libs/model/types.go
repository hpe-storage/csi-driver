package model

import (
	"fmt"
)

const (
	// FsCreateOpt filesystem create type
	FsCreateOpt = "filesystem"
	// FsModeOpt filesystem mode option
	FsModeOpt = "fsMode"
	// FsOwnerOpt filesystem owner option
	FsOwnerOpt = "fsOwner"
)

//type of Scope (volume, group)
type targetScope int

const (
	// VolumeScope scope of the device is volume
	VolumeScope targetScope = 1
	// GroupScope scope of the device is group
	GroupScope targetScope = 2
)

func (e targetScope) String() string {
	switch e {
	case VolumeScope:
		return "volume"
	case GroupScope:
		return "group"
	default:
		return "" // default targetScope is empty which is treated at volume scope
	}
}

// DeviceState : various device states
type DeviceState int

const (
	// FailedState : device is in in failed state
	FailedState DeviceState = iota
	// ActiveState : device is active and running and available for I/O
	ActiveState
	// Orphan : if the multipathd show paths shows them as orphan
	Orphan
	// LunIDConflict : When device has a different serial from inquiry and current vpd page
	LunIDConflict
)

func (e DeviceState) String() string {
	switch e {
	case FailedState:
		return "failed"
	case ActiveState:
		return "active"
	case LunIDConflict:
		return "lunidconflict"
	default:
		return fmt.Sprintf("%d", int(e))
	}
}

// VolumeAccessType : Type of volume access (block, mount)
type VolumeAccessType int

const (
	// BlockType volume access
	BlockType VolumeAccessType = 1
	// MountType volume access
	MountType VolumeAccessType = 2
	// UnknownType volume access
	UnknownType
)

func (e VolumeAccessType) String() string {
	switch e {
	case BlockType:
		return "block"
	case MountType:
		return "mount"
	default:
		return "unknown"
	}
}

// Host : provide host information
type Host struct {
	UUID           string       `json:"id,omitempty"`
	Name           string       `json:"name,omitempty"`
	Domain         string       `json:"domain,omitempty"`
	NodeID         string       `json:"node_id,omitempty"`
	AccessProtocol string       `json:"access_protocol,omitempty"`
	Networks       []*Network   `json:"networks,omitempty"`
	Initiators     []*Initiator `json:"initiators,omitempty"`
}

//Mount struct ID data type is string
type Mount struct {
	ID         string   `json:"id,omitempty"`
	Mountpoint string   `json:"Mountpoint,omitempty"`
	Options    []string `json:"Options,omitempty"`
	Device     *Device  `json:"device,omitempty"`
}

//Network : network interface info for host
type Network struct {
	Name        string `json:"name,omitempty"`
	AddressV4   string `json:"address_v4,omitempty"`
	MaskV4      string `json:"mask_v4,omitempty"`
	BroadcastV4 string `json:",omitempty"`
	Mac         string `json:",omitempty"`
	Mtu         int64  `json:",omitempty"`
	Up          bool
}

//Initiator : Host initiator
type Initiator struct {
	Type string    `json:"type,omitempty"`
	Init []string  `json:"initiator,omitempty"`
	Chap *ChapInfo `json:"chap_info,omitempty"`
}

// ChapInfo : Host initiator CHAP credentials
type ChapInfo struct {
	Name     string `json:"chap_user,omitempty"`
	Password string `json:"chap_password,omitempty"`
}

// IscsiTarget struct
type IscsiTarget struct {
	Name    string
	Address string
	Port    string // 3260
	Tag     string // 2460
	Scope   string // GST or VST
}

//Device struct
type Device struct {
	VolumeID string `json:"volume_id,omitempty"`
	Pathname string `json:"path_name,omitempty"`
	// TODO: SerialNumber does not prepend a "2" so the client has to do it whenever invoking operations like offline or delete.
	// We should probably include the "2" here since that's how the device is seen
	SerialNumber    string       `json:"serial_number,omitempty"`
	Major           string       `json:"major,omitempty"`
	Minor           string       `json:"minor,omitempty"`
	AltFullPathName string       `json:"alt_full_path_name,omitempty"`
	MpathName       string       `json:"mpath_device_name,omitempty"`
	Size            int64        `json:"size,omitempty"` // size in MiB
	Slaves          []string     `json:"slaves,omitempty"`
	IscsiTarget     *IscsiTarget `json:"iscsi_target,omitempty"`
	Hcils           []string     `json:"-"`                      // export it if needed
	TargetScope     string       `json:"target_scope,omitempty"` //GST="group", VST="volume" or empty(older array fiji etc), and no-op for FC
	State           string       `json:"state,omitempty"`        // state of the device needed to verify the device is active
	Filesystem      string       `json:"filesystem,omitempty"`
}

// DevicePartition Partition Info for a Device
type DevicePartition struct {
	Name          string `json:"name,omitempty"`
	Partitiontype string `json:"partition_type,omitempty"`
	Size          int64  `json:"size,omitempty"`
}

// Volume : Thin version of Volume object for Host side
type Volume struct {
	ID             string                 `json:"id,omitempty"`
	Name           string                 `json:"name,omitempty"`
	Size           int64                  `json:"size,omitempty"` // size in bytes
	InUse          bool                   `json:"in_use,omitempty"`
	BaseSnapID     string                 `json:"base_snapshot_id,omitempty"`
	ParentVolID    string                 `json:"parent_volume_id,omitempty"`
	Clone          bool                   `json:"clone,omitempty"`
	Config         map[string]interface{} `json:"config,omitempty"`
	Metadata       []*KeyValue            `json:"metadata,omitempty"`
	SerialNumber   string                 `json:"serial_number,omitempty"`
	AccessProtocol string                 `json:"access_protocol,omitempty"`
	Iqn            string                 `json:"iqn,omitempty"`
	DiscoveryIP    string                 `json:"discovery_ip,omitempty"` // this field needs to be moved out ?
	MountPoint     string                 `json:"Mountpoint,omitempty"`
	Status         map[string]interface{} `json:"status,omitempty"` // interface so that we can map any number of arguments
	Chap           *ChapInfo              `json:"chap_info,omitempty"`
	Networks       []*Network             `json:"networks,omitempty"`
	ConnectionMode string                 `json:"connection_mode,omitempty"`
	LunID          string                 `json:"lun_id,omitempty"`
	TargetScope    string                 `json:"target_scope,omitempty"` //GST="group", VST="volume" or empty(older array fiji etc), and no-op for FC
	IscsiSessions  []*iscsiSession        `json:"iscsi_sessions,omitempty"`
	FcSessions     []*fcSession           `json:"fc_sessions,omitempty"`
}

type fcSession struct {
	InitiatorWwpn string `json:"initiatorWwpn,omitempty"`
}

type iscsiSession struct {
	InitiatorName string `json:"initiatorName,omitempty"`
}

// Snapshot is a snapshot of a volume
type Snapshot struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	VolumeID     string `json:"volume_id,omitempty"`
	VolumeName   string `json:"volume_name,omitempty"`
	Size         int64  `json:"size,omitempty"`
	CreationTime int64  `json:"creation_time,omitempty"`
	ReadyToUse   bool   `json:"ready_to_use,omitempty"`
	InUse        bool   `json:"in_use,omitempty"`
}

// PublishOptions are the options needed to publish a volume
type PublishOptions struct {
	NodeID string `json:"node_id,omitempty"`
}

// PublishInfo is the node side data required to access a volume
type PublishInfo struct {
	SerialNumber string `json:"serial_number,omitempty"`
	AccessInfo
}

// AccessInfo describes the various ways a volume can be accessed
type AccessInfo struct {
	BlockDeviceAccessInfo
	VirtualDeviceAccessInfo
}

// BlockDeviceAccessInfo contains the common fields for accessing a block device
type BlockDeviceAccessInfo struct {
	AccessProtocol string `json:"access_protocol,omitempty"`
	TargetName     string `json:"target_name,omitempty"`
	TargetScope    string `json:"target_scope,omitempty"`
	LunID          int32  `json:"lun_id,omitempty"`
	IscsiAccessInfo
}

// IscsiAccessInfo contains the fields necessary for iSCSI access
type IscsiAccessInfo struct {
	DiscoveryIP  string `json:"discovery_ip,omitempty"`
	ChapUser     string `json:"chap_user,omitempty"`
	ChapPassword string `json:"chap_password,omitempty"`
}

// VirtualDeviceAccessInfo contains the required data to access a virtual device
type VirtualDeviceAccessInfo struct {
}

// FcHostPort FC host port
type FcHostPort struct {
	HostNumber string
	PortWwn    string
	NodeWwn    string
}

// Iface represents iface configuring with port binding
type Iface struct {
	Name    string
	Network *Network
}

// IscsiTargets : array of pointers to IscsiTarget
type IscsiTargets []*IscsiTarget

// HostUUID : test
type HostUUID struct {
	UUID string `json:"id,omitempty"`
}

// Hosts provide information about hosts
type Hosts []*Host

// FilesystemOpts to store fsType, fsMode, fsOwner options
type FilesystemOpts struct {
	Type  string
	Mode  string
	Owner string
}

// PathInfo :
type PathInfo struct {
	UUID     string
	Device   string
	DmState  string
	Hcil     string
	DevState string
	ChkState string
	Checker  string
}

// Token is the authentication token used to communicate with the CSP
type Token struct {
	ID           string `json:"id,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	ArrayIP      string `json:"array_ip,omityempty"`
	SessionToken string `json:"session_token,omitempty"`
}

// Node represents a host that would access volumes through the CSP
type Node struct {
	ID       string    `json:"id,omitempty"`
	Name     string    `json:"name,omitempty"`
	UUID     string    `json:"uuid,omitempty"`
	Iqns     []*string `json:"iqns,omitempty"`
	Networks []*string `json:"networks,omitempty"`
	Wwpns    []*string `json:"wwpns,omitempty"`
}

// KeyValue is a store of key-value pairs
type KeyValue struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

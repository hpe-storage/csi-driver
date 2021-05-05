package model

import (
	"fmt"
	"regexp"
	"strings"
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
	UUID              string              `json:"id,omitempty"`
	Name              string              `json:"name,omitempty"`
	Domain            string              `json:"domain,omitempty"`
	NodeID            string              `json:"node_id,omitempty"`
	AccessProtocol    string              `json:"access_protocol,omitempty"`
	NetworkInterfaces []*NetworkInterface `json:"networks,omitempty"`
	Initiators        []*Initiator        `json:"initiators,omitempty"`
}

//Mount struct ID data type is string
type Mount struct {
	ID         string   `json:"id,omitempty"`
	Mountpoint string   `json:"Mountpoint,omitempty"`
	Options    []string `json:"Options,omitempty"`
	Device     *Device  `json:"device,omitempty"`
}

//NetworkInterface : network interface info for host
type NetworkInterface struct {
	Name        string `json:"name,omitempty"`
	AddressV4   string `json:"address_v4,omitempty"`
	MaskV4      string `json:"mask_v4,omitempty"`
	BroadcastV4 string `json:",omitempty"`
	Mac         string `json:",omitempty"`
	Mtu         int64  `json:",omitempty"`
	Up          bool
	CidrNetwork string
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
	VolumeID            string         `json:"volume_id,omitempty"`
	Pathname            string         `json:"path_name,omitempty"`
	LuksPathname        string         `json:"luks_path_name,omitempty"`
	SerialNumber        string         `json:"serial_number,omitempty"`
	Major               string         `json:"major,omitempty"`
	Minor               string         `json:"minor,omitempty"`
	AltFullPathName     string         `json:"alt_full_path_name,omitempty"`
	AltFullLuksPathName string         `json:"alt_full_luks_path_name,omitempty"`
	MpathName           string         `json:"mpath_device_name,omitempty"`
	Size                int64          `json:"size,omitempty"` // size in MiB
	Slaves              []string       `json:"slaves,omitempty"`
	IscsiTargets        []*IscsiTarget `json:"iscsi_target,omitempty"`
	Hcils               []string       `json:"-"`                      // export it if needed
	TargetScope         string         `json:"target_scope,omitempty"` //GST="group", VST="volume" or empty(older array fiji etc), and no-op for FC
	State               string         `json:"state,omitempty"`        // state of the device needed to verify the device is active
	Filesystem          string         `json:"filesystem,omitempty"`
	StorageVendor       string         `json:"storage_vendor,omitempty"` //3PARdata
}

// DevicePartition Partition Info for a Device
type DevicePartition struct {
	Name          string `json:"name,omitempty"`
	Partitiontype string `json:"partition_type,omitempty"`
	Size          int64  `json:"size,omitempty"`
}

// Volume : Thin version of Volume object for Host side
type Volume struct {
	ID                    string                 `json:"id,omitempty"`
	Name                  string                 `json:"name,omitempty"`
	Size                  int64                  `json:"size,omitempty"` // size in bytes
	Description           string                 `json:"description,omitempty"`
	InUse                 bool                   `json:"in_use,omitempty"` // deprecated for published in the CSP implementation
	Published             bool                   `json:"published,omitempty"`
	BaseSnapID            string                 `json:"base_snapshot_id,omitempty"`
	ParentVolID           string                 `json:"parent_volume_id,omitempty"`
	Clone                 bool                   `json:"clone,omitempty"`
	Config                map[string]interface{} `json:"config,omitempty"`
	Metadata              []*KeyValue            `json:"metadata,omitempty"`
	SerialNumber          string                 `json:"serial_number,omitempty"`
	AccessProtocol        string                 `json:"access_protocol,omitempty"`
	Iqn                   string                 `json:"iqn,omitempty"` // deprecated
	Iqns                  []string               `json:"iqns,omitempty"`
	DiscoveryIP           string                 `json:"discovery_ip,omitempty"` // deprecated
	DiscoveryIPs          []string               `json:"discovery_ips,omitempty"`
	MountPoint            string                 `json:"Mountpoint,omitempty"`
	Status                map[string]interface{} `json:"status,omitempty"` // interface so that we can map any number of arguments
	Chap                  *ChapInfo              `json:"chap_info,omitempty"`
	Networks              []*NetworkInterface    `json:"networks,omitempty"`
	ConnectionMode        string                 `json:"connection_mode,omitempty"`
	LunID                 string                 `json:"lun_id,omitempty"`
	TargetScope           string                 `json:"target_scope,omitempty"` //GST="group", VST="volume" or empty(older array fiji etc), and no-op for FC
	IscsiSessions         []*IscsiSession        `json:"iscsi_sessions,omitempty"`
	FcSessions            []*FcSession           `json:"fc_sessions,omitempty"`
	VolumeGroupId         string                 `json:"volume_group_id"`
	SecondaryArrayDetails string                 `json:"secondary_array_details,omitempty"`
	UsedBytes             int64                  `json:"used_bytes,omitempty"`
	FreeBytes             int64                  `json:"free_bytes,omitempty"`
	EncryptionKey         string                 `json:"encryption_key,omitempty"`
}

func (v Volume) TargetNames() []string {
	if v.Iqn != "" {
		return []string{v.Iqn}
	}
	return v.Iqns
}

// Workaround NOS 5.0.x vs 5.1.x responses with different case
// FcSession info
type FcSession struct {
	InitiatorWwpn       string `json:"initiator_wwpn,omitempty"`
	InitiatorWwpnLegacy string `json:"initiatorWwpn,omitempty"`
}

// IscsiSession info
type IscsiSession struct {
	InitiatorName       string `json:"initiator_name,omitempty"`
	InitiatorNameLegacy string `json:"initiatorName,omitempty"`
	InitiatorIP         string `json:"initiator_ip_addr,omitempty"`
}

func (s FcSession) InitiatorWwpnStr() string {
	if s.InitiatorWwpnLegacy != "" {
		return s.InitiatorWwpnLegacy
	}
	return s.InitiatorWwpn
}

func (s IscsiSession) InitiatorNameStr() string {
	if s.InitiatorNameLegacy != "" {
		return s.InitiatorNameLegacy
	}
	return s.InitiatorName
}

// Snapshot is a snapshot of a volume
type Snapshot struct {
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Description  string                 `json:"description,omitempty"`
	VolumeID     string                 `json:"volume_id,omitempty"`
	VolumeName   string                 `json:"volume_name,omitempty"`
	Size         int64                  `json:"size,omitempty"`
	CreationTime int64                  `json:"creation_time,omitempty"`
	ReadyToUse   bool                   `json:"ready_to_use,omitempty"`
	InUse        bool                   `json:"in_use,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty"`
}

type VolumeGroup struct {
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Description  string                 `json:"description,omitempty"`
	CreationTime int64                  `json:"creation_time,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty"`
}

type SnapshotGroup struct {
	ID                    string                 `json:"id,omitempty"`
	Name                  string                 `json:"name,omitempty"`
	SourceVolumeGroupID   string                 `json:"volume_group_id,omitempty"`
	SourceVolumeGroupName string                 `json:"volume_group_name,omitempty"`
	Snapshots             []*Snapshot            `json:"snapshots,omitempty"`
	CreationTime          int64                  `json:"creation_time,omitempty"`
	Config                map[string]interface{} `json:"config,omitempty"`
}

// PublishOptions are the options needed to publish a volume
type PublishOptions struct {
	HostUUID       string `json:"host_uuid,omitempty"`
	AccessProtocol string `json:"access_protocol,omitempty"`
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
	AccessProtocol string   `json:"access_protocol,omitempty"`
	TargetNames    []string `json:"target_names,omitempty"`
	LunID          int32    `json:"lun_id,omitempty"`
	SecondaryBackendDetails
	IscsiAccessInfo
}

// Information of LUN id, IQN, discovery IP's the secondary array
type SecondaryBackendDetails struct {
	PeerArrayDetails []*SecondaryLunInfo
}

// Information of the each secondary array
type SecondaryLunInfo struct {
	LunID       int32    `json:"lun_id,omitempty""`
	TargetNames []string `json:"target_names,omitempty"`
	IscsiAccessInfo
}

// IscsiAccessInfo contains the fields necessary for iSCSI access
type IscsiAccessInfo struct {
	DiscoveryIPs []string `json:"discovery_ips,omitempty"`
	ChapUser     string   `json:"chap_user,omitempty"`
	ChapPassword string   `json:"chap_password,omitempty"`
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
	Name             string
	NetworkInterface *NetworkInterface
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
	Type       string
	Mode       string
	Owner      string
	CreateOpts string
}

// GetCreateOpts returns a clean array that can be passed to the command line
func (f FilesystemOpts) GetCreateOpts() []string {
	cleanCharRegex := regexp.MustCompile(`[^a-zA-Z0-9=, \-]`)
	singleSpacesRegex := regexp.MustCompile(`\s+`)

	clean := cleanCharRegex.ReplaceAllString(strings.Trim(f.CreateOpts, " "), "")
	cleanSlice := strings.Split(singleSpacesRegex.ReplaceAllString(clean, " "), " ")
	if len(cleanSlice) == 1 && cleanSlice[0] == "" {
		return nil
	}
	return cleanSlice
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
	ID           string    `json:"id,omitempty"`
	Name         string    `json:"name,omitempty"`
	UUID         string    `json:"uuid,omitempty"`
	Iqns         []*string `json:"iqns,omitempty"`
	Networks     []*string `json:"networks,omitempty"`
	Wwpns        []*string `json:"wwpns,omitempty"`
	ChapUser     string    `json:"chap_user,omitempty"`
	ChapPassword string    `json:"chap_password,omitempty"`
}

// KeyValue is a store of key-value pairs
type KeyValue struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

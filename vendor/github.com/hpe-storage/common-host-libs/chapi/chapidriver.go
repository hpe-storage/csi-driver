// Copyright 2019 Hewlett Packard Enterprise Development LP

package chapi

import (
	"github.com/hpe-storage/common-host-libs/model"
)

// Driver provides a common interface for host related operations
type Driver interface {
	GetHosts() (*model.Hosts, error)
	GetHostInfo() (*model.Host, error)
	GetHostInitiators() ([]*model.Initiator, error)
	GetHostNetworks() ([]*model.NetworkInterface, error)
	GetHostNameAndDomain() ([]string, error)
	CreateDevices(volumes []*model.Volume) ([]*model.Device, error)
	GetDevice(volume *model.Volume) (*model.Device, error)
	DeleteDevice(device *model.Device) error
	OfflineDevice(device *model.Device) error
	MountDevice(device *model.Device, mountPoint string, mountOptions []string, fsOpts *model.FilesystemOpts) (*model.Mount, error) // Idempotent
	MountNFSVolume(source string, target string, mountOptions []string, nfsType string) error                                       // Idempotent
	BindMount(mountPoint string, newMountPoint string, rbind bool) error                                                            // Idempotent
	BindUnmount(mountPoint string) error                                                                                            // Idempotent
	UnmountDevice(device *model.Device, mountPoint string) (*model.Mount, error)                                                    // Idempotent
	UnmountFileSystem(mountPoint string) (*model.Mount, error)                                                                      // Idempotent
	GetMounts(serialNumber string) ([]*model.Mount, error)
	GetMountsForDevice(device *model.Device) ([]*model.Mount, error)
	ExpandDevice(targetPath string, volAccessType model.VolumeAccessType) error
	IsBlockDevice(devicePath string) (bool, error)
	GetBlockSizeBytes(devicePath string) (int64, error)
}

// Copyright 2019 Hewlett Packard Enterprise Development LP

package chapi

import (
	"fmt"

	"github.com/hpe-storage/common-host-libs/model"
)

// MacDriver ... Mac implementation of the CHAPI driver
type MacDriver struct {
}

// GetHostInfo returns host name, domain, and network interfaces
func (driver *MacDriver) GetHostInfo() (*model.Host, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetHostInitiators reports the initiators on this host
func (driver *MacDriver) GetHostInitiators() ([]*model.Initiator, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetHosts returns information about this host within an array.  Not sure why but we should probably fix that.
func (driver *MacDriver) GetHosts() (*model.Hosts, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetMountOptions reports mount options that have been applied to the device on the host
func (driver *MacDriver) GetMountOptions(device *model.Device, mountPoint string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetHostNetworks reports the networks on this host
func (driver *MacDriver) GetHostNetworks() ([]*model.NetworkInterface, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetMounts reports all mounts on this host
func (driver *MacDriver) GetMounts(serialNumber string) ([]*model.Mount, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetMountsForDevice reports all mounts for the given device on the host
func (driver *MacDriver) GetMountsForDevice(device *model.Device) ([]*model.Mount, error) {
	return nil, fmt.Errorf("not implemented")
}

// MountDevice mounts the given device to the given mount point
func (driver *MacDriver) MountDevice(device *model.Device, mountPoint string, mountOptions []string, fsOpts *model.FilesystemOpts) (*model.Mount, error) {
	return nil, fmt.Errorf("not implemented")
}

// OfflineDevice will offline the given device from the host
func (driver *MacDriver) OfflineDevice(device *model.Device) error {
	return fmt.Errorf("not implemented")
}

// ReMountWithOptions remounts the existing mountpoint with options
func (driver *MacDriver) ReMountWithOptions(mountPoint string, options []string) error {
	return fmt.Errorf("not implemented")
}

// SetFilesystemOptions applies the given FS options on the filesystem of the device
func (driver *MacDriver) SetFilesystemOptions(mountPoint string, options *model.FilesystemOpts) error {
	return fmt.Errorf("not implemented")
}

// UnmountDevice unmounts the given device from the given mount point
func (driver *MacDriver) UnmountDevice(device *model.Device, mountPoint string) (*model.Mount, error) {
	return nil, fmt.Errorf("not implemented")
}

// UnmountFileSystem will unmount the given mount point
func (driver *MacDriver) UnmountFileSystem(mountPoint string) (*model.Mount, error) {
	return nil, fmt.Errorf("not implemented")
}

// SetMountOptions applies mount options to the mount point on the host
func (driver *MacDriver) SetMountOptions(device *model.Device, mountPoint string, options []string) error {
	return fmt.Errorf("not implemented")
}

// GetHostNameAndDomain reports the host name and domain
func (driver *MacDriver) GetHostNameAndDomain() ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

// BindMountDevice bind mounts the existing mountpoint to the given mount point
func (driver *MacDriver) BindMountDevice(mountPoint string, newMountPoint string, rbind bool) error {
	return fmt.Errorf("not implemented")
}

// GetDevice will return device matching given volume serial
func (driver *MacDriver) GetDevice(volume *model.Volume) (*model.Device, error) {
	return nil, fmt.Errorf("not implemented")
}

// CreateDevices will create devices on this host based on the volume details provided
func (driver *MacDriver) CreateDevices(volumes []*model.Volume) ([]*model.Device, error) {
	return nil, fmt.Errorf("not implemented")
}

// CreateFilesystemOnDevice writes the given filesystem on the given device
func (driver *MacDriver) CreateFilesystemOnDevice(device *model.Device, filesystemType string) error {
	return fmt.Errorf("not implemented")
}

// DeleteDevice will delete the given device from the host
func (driver *MacDriver) DeleteDevice(device *model.Device) error {
	return fmt.Errorf("not implemented")
}

// GetFilesystemFromDevice writes the given filesystem on the given device
func (driver *MacDriver) GetFilesystemFromDevice(device *model.Device) (*model.FilesystemOpts, error) {
	return nil, fmt.Errorf("not implemented")
}

// BindMount bind mounts the existing mountpoint to the given mount point
func (driver *MacDriver) BindMount(mountPoint string, newMountPoint string, rbind bool) error {
	return fmt.Errorf("not implemented")
}

// BindUnmount unmounts the given bind mount
func (driver *MacDriver) BindUnmount(mountPoint string) error {
	return fmt.Errorf("not implemented")
}

// ExpandDevice will expand the given device/filesystem on the host
func (driver *MacDriver) ExpandDevice(targetPath string, volAccessType model.VolumeAccessType) error {
	return fmt.Errorf("not implemented")
}

// MountNFSVolume mounts NFS share onto given target path
func (driver *MacDriver) MountNFSVolume(source string, targetPath string, mountOptions []string, nfsType string) error {
	return nil
}

// IsBlockDevice will check if the given path is a block device
func (driver *MacDriver) IsBlockDevice(devicePath string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

// GetBlockSizeBytes returns the size of the block device
func (driver *MacDriver) GetBlockSizeBytes(devicePath string) (int64, error) {
	return -1, fmt.Errorf("not implemented")
}

// Copyright 2019 Hewlett Packard Enterprise Development LP

package chapi

import (
	uuid "github.com/satori/go.uuid"

	"github.com/hpe-storage/common-host-libs/model"
)

// FakeDriver ... the name says it all
type FakeDriver struct {
}

// GetHosts returns information about this host within an array.  Not sure why but we should probably fix that.
func (driver *FakeDriver) GetHosts() (*model.Hosts, error) {
	hosts := &model.Hosts{
		&model.Host{UUID: uuid.NewV4().String()},
	}
	return hosts, nil
}

// GetHostInfo returns host name, domain, and network interfaces
func (driver *FakeDriver) GetHostInfo() (*model.Host, error) {
	host := &model.Host{
		Name:   "host1",
		Domain: "host1.domain.com",
	}
	return host, nil
}

// GetHostInitiators reports the initiators on this host
func (driver *FakeDriver) GetHostInitiators() ([]*model.Initiator, error) {
	return nil, nil
}

// GetHostNetworks reports the networks on this host
func (driver *FakeDriver) GetHostNetworks() ([]*model.NetworkInterface, error) {
	return nil, nil
}

// GetHostNameAndDomain reports the host name and domain
func (driver *FakeDriver) GetHostNameAndDomain() ([]string, error) {
	return []string{
		"host1",
		"host1.domain.com",
	}, nil
}

// GetDevice will return device matching given volume serial
func (driver *FakeDriver) GetDevice(volume *model.Volume) (*model.Device, error) {
	device := &model.Device{
		SerialNumber:    volume.SerialNumber,
		AltFullPathName: "/dev/mapper/fakeMpath",
	}
	return device, nil
}

// CreateDevices will create devices on this host based on the volume details provided
func (driver *FakeDriver) CreateDevices(volumes []*model.Volume) ([]*model.Device, error) {
	device := &model.Device{
		SerialNumber: "fakeSerialNumber",
	}
	return []*model.Device{device}, nil
}

// CreateFilesystemOnDevice writes the given filesystem to the device with the given serial number
func (driver *FakeDriver) CreateFilesystemOnDevice(device *model.Device, filesytemType string) error {
	return nil
}

// SetFilesystemOptions applies given filesystem options to the filesystem on the device
func (driver *FakeDriver) SetFilesystemOptions(mountPoint string, fsOpts *model.FilesystemOpts) error {
	return nil
}

// GetFilesystemOptionsFromDevice retrieves the filesystem options from the filesystem on the device
func (driver *FakeDriver) GetFilesystemOptionsFromDevice(device *model.Device, mountPoint string) (*model.FilesystemOpts, error) {
	return &model.FilesystemOpts{
		Type: "",
	}, nil
}

// GetMountsForDevice reports all mounts for the device on this host
func (driver *FakeDriver) GetMountsForDevice(device *model.Device) ([]*model.Mount, error) {
	mount := &model.Mount{
		Mountpoint: "/fakeMountPoint",
		Device:     device,
	}
	return []*model.Mount{mount}, nil
}

// SetMountOptions applies the mount options to the mountpoint of the device
func (driver *FakeDriver) SetMountOptions(device *model.Device, mountPoint string, options []string) error {
	return nil
}

// GetMountOptions retrieves the mount options of the mountpoint of the device
func (driver *FakeDriver) GetMountOptions(device *model.Device, mountPoint string) ([]string, error) {
	return []string{}, nil
}

// BindMount binds the existing mountpoint to another mountpoint
func (driver *FakeDriver) BindMount(mountPoint string, newMountPoint string, rbind bool) error {
	return nil
}

// BindUnmount unmounts the given bind mount
func (driver *FakeDriver) BindUnmount(mountPoint string) error {
	return nil
}

// GetMounts reports all mounts on this host
func (driver *FakeDriver) GetMounts(serialNumber string) ([]*model.Mount, error) {
	device := &model.Device{
		SerialNumber: "fakeSerialNumber",
	}
	mount := &model.Mount{
		Mountpoint: "/fakeMountPoint",
		Device:     device,
	}
	return []*model.Mount{mount}, nil
}

// MountDevice mounts the given device to the given mount point
func (driver *FakeDriver) MountDevice(device *model.Device, mountPoint string, options []string, fsOpts *model.FilesystemOpts) (*model.Mount, error) {
	return &model.Mount{
		Mountpoint: "/fakeMountPoint",
		Device:     device,
	}, nil
}

// UnmountDevice unmounts the given device from the given mount point
func (driver *FakeDriver) UnmountDevice(device *model.Device, mountPoint string) (*model.Mount, error) {
	return nil, nil
}

// UnmountFileSystem will unmount the given mount point
func (driver *FakeDriver) UnmountFileSystem(mountPoint string) (*model.Mount, error) {
	return nil, nil
}

// DeleteDevice will delete the given device from the host
func (driver *FakeDriver) DeleteDevice(device *model.Device) error {
	return nil
}

// OfflineDevice will offline the given device from the host
func (driver *FakeDriver) OfflineDevice(device *model.Device) error {
	return nil
}

// ExpandDevice will expand the given device/filesystem on the host
func (driver *FakeDriver) ExpandDevice(targetPath string, volAccessType model.VolumeAccessType) error {
	return nil
}

// MountNFSVolume mounts NFS share onto given target path
func (driver *FakeDriver) MountNFSVolume(source string, targetPath string, mountOptions []string, nfsType string) error {
	return nil
}

// IsBlockDevice will check if the given path is a block device
func (driver *FakeDriver) IsBlockDevice(devicePath string) (bool, error) {
	return false, nil
}

// GetBlockSizeBytes returns the size of the block device
func (driver *FakeDriver) GetBlockSizeBytes(devicePath string) (int64, error) {
	return -1, nil
}

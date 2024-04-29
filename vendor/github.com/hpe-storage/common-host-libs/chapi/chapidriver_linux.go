// Copyright 2019 Hewlett Packard Enterprise Development LP

package chapi

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"sort"
	"sync"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
	uuid "github.com/satori/go.uuid"
)

// LinuxDriver ... Linux implementation of the CHAPI driver
type LinuxDriver struct {
}

const (
	configDir         = "/etc/hpe-storage/"
	nodeFileName      = "node.gob"
	defaultFileSystem = "xfs"
	hostFile          = configDir + nodeFileName
)

var (
	fileLock = &sync.Mutex{}
)

// getOrCreateHostFile will check if host file is already present or else will create one
func getOrCreateHostFile() (string, error) {
	fileLock.Lock()
	defer fileLock.Unlock()

	if _, err := os.Stat(hostFile); os.IsNotExist(err) {
		os.MkdirAll(configDir, 0640)
		// hostFile does not exist... create one with unique id
		id, err := generateUniqueNodeId()
		if err != nil {
			return "", err
		}
		// persist uuid to gob file
		log.Infof("Writing uuid to file:%s uuid:%s", hostFile, id)
		hosts := model.Hosts{
			&model.Host{UUID: id},
		}
		err = util.FileSaveGob(hostFile, hosts)
		if err != nil {
			return "", err
		}
	}
	return hostFile, nil
}

func generateUniqueNodeId() (string, error) {
	// get host name
	hostName, err := os.Hostname()
	if err != nil {
		return "", err
	}
	macs, err := getMacAddresses()
	if err != nil {
		return "", err
	}

	// sort mac addresses as best attempt to get same address even if node.gob is deleted
	sort.Strings(macs)

	// create a unique id using mac address and host name using Md5 hash
	// https://github.com/hpe-storage/csi-driver/issues/270
	idStr := util.GetMD5HashOfTwoStrings(strings.Replace(macs[0], ":", "", -1), hostName)
	if len(idStr) < 32 {
		// pad with zeroes for minimum length
		idStr += strings.Repeat("0", 32-len(idStr))
	}
	id, err := uuid.FromString(idStr[0:32])
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func getMacAddresses() ([]string, error) {
	// get mac address
	nics, err := linux.GetNetworkInterfaces()
	if err != nil {
		return nil, err
	}
	var macs []string
	for _, nic := range nics {
		// ignore mac-id's with all 0's (loopback etc)
		if nic.Mac != "" && strings.Count(strings.Replace(nic.Mac, ":", "0", -1), "0") != len(nic.Mac) {
			macs = append(macs, nic.Mac)
		}
	}
	if len(macs) == 0 {
		return nil, errors.New("No networks found on the host")
	}
	return macs, nil
}

// GetHosts returns information about this host within an array.  Not sure why but we should probably fix that.
func (driver *LinuxDriver) GetHosts() (*model.Hosts, error) {
	hosts := new(model.Hosts)
	hostsFile, err := getOrCreateHostFile()
	if err != nil {
		return nil, err
	}
	err = util.FileloadGob(hostsFile, hosts)
	if err != nil {
		return nil, err
	}
	return hosts, nil
}

// GetHostInfo returns host name, domain, and network interfaces
func (driver *LinuxDriver) GetHostInfo() (*model.Host, error) {
	hostAndDomain, err := linux.GetHostNameAndDomain()
	if err != nil {
		log.Errorln("GetHostInfo Err:", err)
		if len(hostAndDomain) != 2 {
			return nil, errors.New("unable to fetch hostname and domain name")
		}
		return nil, err
	}

	nics, err := linux.GetNetworkInterfaces()
	if err != nil {
		log.Errorln("GetNetworkInterfaces returned error:", err)
		if len(nics) == 0 {
			return nil, errors.New("No Network found on the host")
		}
		return nil, err
	}

	host := &model.Host{
		Name:              hostAndDomain[0],
		Domain:            hostAndDomain[1],
		NetworkInterfaces: nics,
	}
	return host, nil
}

// GetHostInitiators reports the initiators on this host
func (driver *LinuxDriver) GetHostInitiators() ([]*model.Initiator, error) {
	return linux.GetInitiators()
}

// GetHostNetworks reports the networks on this host
func (driver *LinuxDriver) GetHostNetworks() ([]*model.NetworkInterface, error) {
	return linux.GetNetworkInterfaces()
}

// GetHostNameAndDomain reports the host name and domain
func (driver *LinuxDriver) GetHostNameAndDomain() ([]string, error) {
	return linux.GetHostNameAndDomain()
}

// CreateDevices will create devices on this host based on the volume details provided
func (driver *LinuxDriver) CreateDevices(volumes []*model.Volume) ([]*model.Device, error) {
	return linux.CreateLinuxDevices(volumes)
}

func (driver *LinuxDriver) GetDevice(volume *model.Volume) (*model.Device, error) {
	return linux.GetDeviceFromVolume(volume)
}

// CreateFilesystemOnDevice writes the given filesystem on the given device
func (driver *LinuxDriver) CreateFilesystemOnDevice(device *model.Device, filesystemType string) error {
	log.Tracef(">>>>> CreateFilesystemOnDevice, device: %+v, type: %s", device, filesystemType)
	defer log.Trace("<<<<< CreateFilesystemOnDevice")

	if filesystemType == "" {
		log.Trace("Using default filesystem type ", defaultFileSystem)
		filesystemType = defaultFileSystem
	}
	log.Tracef("Creating filesystem %s on device path %s", filesystemType, device.AltFullPathName)
	if err := linux.RetryCreateFileSystem(device.AltFullPathName, filesystemType); err != nil {
		log.Errorf("Failed to create filesystem %s on device with path %s", filesystemType, device.AltFullPathName)
		return err
	}
	return nil
}

// SetFilesystemOptions applies the given FS options on the filesystem of the device
func (driver *LinuxDriver) SetFilesystemOptions(mountPoint string, options *model.FilesystemOpts) error {
	log.Tracef(">>>>> SetFilesystemOptions, mountPoint: %s, options: %+v", mountPoint, options)
	defer log.Trace("<<<<< SetFilesystemOptions")

	log.Tracef("Setting fileSystem options %+v on mountpoint %s", options, mountPoint)
	if err := linux.SetFilesystemOptions(mountPoint, options); err != nil {
		log.Errorf("Error while applying filesystem options %+v on mountpoint %s", options, mountPoint)
		return err
	}
	return nil
}

// GetFilesystemFromDevice writes the given filesystem on the given device
func (driver *LinuxDriver) GetFilesystemFromDevice(device *model.Device) (*model.FilesystemOpts, error) {
	log.Tracef(">>>>> GetFilesystemFromDevice, device: %+v", device)
	defer log.Trace("<<<<< GetFilesystemFromDevice")

	log.Tracef("Getting fileSystem options from the device path %s", device.AltFullPathName)
	fsOpts, err := linux.GetFilesystemOptions(device, "")
	if err != nil {
		log.Errorf("Error while retrieving filesystem options on device path %s", device.AltFullPathName)
		return nil, err
	}
	return fsOpts, nil
}

// GetMounts reports all mounts on this host
func (driver *LinuxDriver) GetMounts(serialNumber string) ([]*model.Mount, error) {
	log.Trace(">>>>> GetMounts, serialNumber: ", serialNumber)
	defer log.Trace("<<<<< GetMounts")
	devices, err := linux.GetLinuxDmDevices(false, util.GetVolumeObject(serialNumber, ""))
	if err != nil {
		return nil, err
	}
	return linux.GetMountPointsForDevices(devices)
}

// GetMountsForDevice reports all mounts for the given device on the host
func (driver *LinuxDriver) GetMountsForDevice(device *model.Device) ([]*model.Mount, error) {
	log.Trace(">>>>> GetMountsForDevice, device: ", device)
	defer log.Trace("<<<<< GetMountsForDevice")

	devices := []*model.Device{device}
	return linux.GetMountPointsForDevices(devices)
}

func (driver *LinuxDriver) MountNFSVolume(source string, targetPath string, mountOptions []string, nfsType string) error {
	log.Tracef(">>>>> MountNFSVolume called with source %s target %s options %v: ", source, targetPath, mountOptions)
	defer log.Trace("<<<<< MountNFSVolume")

	err := linux.MountNFSShare(source, targetPath, mountOptions, nfsType)
	if err != nil {
		return fmt.Errorf("Error mounting nfs share %s at %s, err %s", source, targetPath, err.Error())
	}
	return nil
}

// MountDevice mounts the given device to the given mount point. This must be idempotent.
func (driver *LinuxDriver) MountDevice(device *model.Device, mountPoint string, mountOptions []string, fsOpts *model.FilesystemOpts) (*model.Mount, error) {
	log.Tracef(">>>>> MountDevice, device: %+v, mountPoint: %s, mountOptions: %v, fsOpts: %+v", device, mountPoint, mountOptions, fsOpts)
	defer log.Trace("<<<<< MountDevice")

	// Setup FS if requested
	if fsOpts != nil {
		if fsOpts.GetCreateOpts() != nil {
			// Create filesystem if not present
			if err := linux.SetupFilesystemWithOptions(device, fsOpts.Type, fsOpts.GetCreateOpts()); err != nil {
				return nil, fmt.Errorf("Error creating filesystem %s on device with serialNumber %s, %v", fsOpts.Type, device.SerialNumber, err.Error())
			}
		} else {
			// Create filesystem if not present
			if err := linux.SetupFilesystem(device, fsOpts.Type); err != nil {
				return nil, fmt.Errorf("Error creating filesystem %s on device with serialNumber %s, %v", fsOpts.Type, device.SerialNumber, err.Error())
			}
		}
		log.Tracef("Filesystem %+v setup successful,", fsOpts)
	}

	// Setup mountpoint (Create mountpoint and apply mount options)
	mount, err := linux.SetupMount(device, mountPoint, mountOptions)
	if err != nil {
		log.Errorf("Failed to setup mountpoint %s for device %s, err: %v", mountPoint, device.AltFullPathName, err.Error())
		return nil, err
	}
	log.Tracef("Mountpoint %v setup successful,", mountPoint)

	// Setup FS options if requested
	if fsOpts != nil {
		// Set FS options
		err = linux.SetFilesystemOptions(mountPoint, fsOpts)
		if err != nil {
			log.Errorf("Unable to set filesystem options %v for mountpoint %s, err: %s", fsOpts, mountPoint, err.Error())
			return nil, err
		}
		log.Tracef("Filesystem options %+v applied successfully,", fsOpts)
	}

	return mount, nil
}

// BindMount bind mounts the existing mountpoint to the given mount point
func (driver *LinuxDriver) BindMount(mountPoint string, newMountPoint string, rbind bool) error {
	log.Tracef(">>>>> BindMount, mountPoint: %s, newMountPoint: %s, rbind: %v", mountPoint, newMountPoint, rbind)
	defer log.Trace("<<<<< BindMount")

	// Bind Mount
	return linux.RetryBindMount(mountPoint, newMountPoint, rbind)
}

// BindUnmount unmounts the given bind mount
func (driver *LinuxDriver) BindUnmount(mountPoint string) error {
	log.Tracef(">>>>> BindUnmount, mountPoint: %s", mountPoint)
	defer log.Trace("<<<<< BindUnmount")

	// Unmount given bind mount
	return linux.RetryBindUnmount(mountPoint)
}

// UnmountDevice unmounts the given device from the given mount point
func (driver *LinuxDriver) UnmountDevice(device *model.Device, mountPoint string) (*model.Mount, error) {
	log.Tracef(">>>>> UnmountDevice, device: %+v, mountPoint: %s", device, mountPoint)
	defer log.Trace("<<<<< UnmountDevice")

	// Get all mounts for the device
	mounts, err := driver.GetMountsForDevice(device)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving mounts for device %s", device.AltFullPathName)
	}

	// Check if mountPoint still exists and unmount it
	for _, mount := range mounts {
		if mount.Mountpoint == mountPoint {
			log.Tracef("Unmounting device %s from mountpoint %s", device.AltFullPathName, mountPoint)
			return linux.UnmountDevice(device, mountPoint)
		}
	}
	return nil, nil
}

// UnmountFileSystem will unmount the given mount point
func (driver *LinuxDriver) UnmountFileSystem(mountPoint string) (*model.Mount, error) {
	return linux.UnmountFileSystem(mountPoint)
}

// DeleteDevice will delete the given device from the host
func (driver *LinuxDriver) DeleteDevice(device *model.Device) error {
	return linux.DeleteDevice(device)
}

// OfflineDevice will offline the given device from the host
func (driver *LinuxDriver) OfflineDevice(device *model.Device) error {
	return linux.OfflineDevice(device)
}

// ExpandDevice will expand the given device/filesystem on the host
func (driver *LinuxDriver) ExpandDevice(targetPath string, volAccessType model.VolumeAccessType) error {
	return linux.ExpandDevice(targetPath, volAccessType)
}

// IsBlockDevice will check if the given path is a block device
func (driver *LinuxDriver) IsBlockDevice(devicePath string) (bool, error) {
	return linux.IsBlockDevice(devicePath)
}

// GetBlockSizeBytes returns the size of the block device
func (driver *LinuxDriver) GetBlockSizeBytes(devicePath string) (int64, error) {
	return linux.GetBlockSizeBytes(devicePath)
}

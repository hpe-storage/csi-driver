// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/stringformat"
	"github.com/hpe-storage/common-host-libs/util"
)

var (
	updateDbConf           = "/etc/updatedb.conf"
	prunePathsPattern      = "^PRUNEPATHS\\s*=\\s*\"(?P<paths>.*)\""
	defaultFSCreateTimeout = 300 /* 5 minutes */
	lsof                   = "lsof"
	blkid                  = "blkid"
	errCurrentlyMounted    = "is currently mounted"
	procMounts             = "/proc/mounts"
)

// FsType indicates the filesystem type of mounted device
type FsType int

const (
	// Xfs xfs filesystem type
	Xfs FsType = 1 + iota
	// Btrfs B-Tree filesystem type
	Btrfs
	// Ext2 ext2 filesystem type
	Ext2
	// Ext3 ext3 filesytem type
	Ext3
	// Ext4 ext3 filesytem type
	Ext4
)

func (fs FsType) String() string {
	switch fs {
	case Xfs:
		return "xfs"
	case Btrfs:
		return "btrfs"
	case Ext2:
		return "ext2"
	case Ext3:
		return "ext3"
	case Ext4:
		return "ext4"
	}
	return ""
}

//Mount struct
type Mount struct {
	ID         uint64        `json:"id,omitempty"`
	Mountpoint string        `json:"mount_point,omitempty"`
	Device     *model.Device `json:"device,omitempty"`
}

const (
	fsxfscommand   = "mkfs.xfs"
	mountUUIDErr   = 32
	fsext2command  = "mkfs.ext2"
	fsext3command  = "mkfs.ext3"
	fsext4command  = "mkfs.ext4"
	fsbtrfscommand = "mkfs.btrfs"
	defaultNFSType = "nfs4"
)

// HashMountID : get hash of the string
func HashMountID(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return fmt.Sprint(h.Sum64())
}

// GetMountPointsForDevices : get the mount point for the Device
// nolint : gocyclo
func GetMountPointsForDevices(devices []*model.Device) ([]*model.Mount, error) {
	log.Tracef(">>>>> GetMountPointsForDevices, devices: %+v", devices)
	defer log.Trace("<<<<< GetMountPointsForDevices")

	var mounts []*model.Mount
	devToMounts := make(map[string][]string)
	var args []string
	out, _, err := util.ExecCommandOutput(mountCommand, args)
	if err != nil {
		return nil, err
	}
	mountLines := strings.Split(out, "\n")
	for _, line := range mountLines {
		entry := strings.Fields(line)
		if len(entry) > 3 {
			devToMounts[entry[0]] = append(devToMounts[entry[0]], entry[2])
		}
	}

	for _, dev := range devices {
		log.Tracef("Checking mount for device %+v ", dev)
		devPath := dev.AltFullPathName
		if dev.AltFullLuksPathName != "" {
			devPath = dev.AltFullLuksPathName
		}
		//if mountPoints, ok := devToMounts[dev.AltFullPathName]; ok {
		if mountPoints, ok := devToMounts[devPath]; ok {
			for _, mountPoint := range mountPoints {
				mount := &model.Mount{
					Mountpoint: mountPoint,
					Device:     dev,
					ID:         HashMountID(mountPoint + dev.SerialNumber),
				}
				log.Trace(mount.ID)
				mounts = append(mounts, mount)
			}
		} else {
			// If the device does not exist in /proc/mounts then check for partition
			log.Tracef("Checking partition for device %+v", dev)
			var devicePartitionInfos []model.DevicePartition
			// fuzzy search to see if it is worth getting partitions
			for k := range devToMounts {
				if strings.HasPrefix(k, dev.AltFullPathName) {
					log.Debugf("%s matched for device %s. checking for partition for device", k, dev.AltFullPathName)
					devicePartitionInfos, _ = GetPartitionInfo(dev)
					break
				}
			}
			// if there are any partitions, loop through them to see if one is mounted
			if devicePartitionInfos != nil && len(devicePartitionInfos) != 0 {
				for _, partition := range devicePartitionInfos {
					if mountPoints, ok := devToMounts[devMapperPath+partition.Name]; ok {
						log.Debugf("A partition of %v is mounted on %#v", dev, mountPoints)
						for _, mountPoint := range mountPoints {
							mount := &model.Mount{
								Mountpoint: mountPoint,
								Device:     dev,
								ID:         HashMountID(mountPoint + dev.SerialNumber),
							}
							log.Trace(mount.ID)
							mounts = append(mounts, mount)
						}
					}
				}
			}
		}
	}
	return mounts, nil
}

// GetMountOptionsForDevice : get options used for mount point for the Device
func GetMountOptionsForDevice(device *model.Device) (options []string, err error) {
	log.Trace("GetMountOptionsForDevice called with device ", device.AltFullPathName)
	mountLines, err := util.FileGetStrings(procMounts)
	if err != nil {
		return nil, err
	}
	for _, line := range mountLines {
		entry := strings.Fields(line)
		if len(entry) > 4 {
			if strings.TrimSpace(entry[0]) == device.AltFullPathName {
				options = strings.Split(entry[3], ",")
				log.Trace("Got FS options ", options)
				return options, nil
			}
		}
	}
	return nil, nil
}

// GetMountOptions : get options used for mount point for the Device
func GetMountOptions(devPath string, mountPoint string) (options []string, err error) {
	log.Tracef(">>>>> GetMountOptions,  devicePath: %s, mountPoint: %s", devPath, mountPoint)
	defer log.Trace("<<<<< GetMountOptions")

	mountLines, err := util.FileGetStrings(procMounts)
	if err != nil {
		return nil, err
	}
	for _, line := range mountLines {
		entry := strings.Fields(line)
		if len(entry) > 4 {
			if strings.TrimSpace(entry[0]) == devPath && strings.TrimSpace(entry[1]) == mountPoint {
				log.Debugf("Found Mount Entry: %s", line)
				options = strings.Split(entry[3], ",")
				return options, nil
			}
		}
	}
	return nil, nil
}

// SetMountOptions : get options used for mount point for the Device
func SetMountOptions(devPath string, mountPoint string, options []string) error {
	log.Tracef(">>>>> SetMountOptions,  devicePath: %s, mountPoint: %s, options: %v", devPath, mountPoint, options)
	defer log.Trace("<<<<< SetMountOptions")

	// Perform remount while applying mount options for an existing mountPoint
	if err := RemountWithOptions(mountPoint, options); err != nil {
		return err
	}
	return nil
}

// CreateFileSystemOnDevice : create filesystem on device with serialnumber :serialnumber
// nolint: gocyclo
func CreateFileSystemOnDevice(serialnumber string, fileSystemType string) (dev *model.Device, err error) {
	log.Tracef("CreateFileSystemOnDevice called with :%s %s", serialnumber, fileSystemType)
	var devices []*model.Device
	for i := 1; i <= countdownTicker; i++ {
		devices, err = GetLinuxDmDevices(true, util.GetVolumeObject(serialnumber, ""))
		if err != nil {
			log.Debugf("error to retrieve active paths for %s, retrying, count=%d", serialnumber, i)
			continue
		}
		if len(devices) >= 0 {
			log.Debugf("device %v found ", devices[0])
			break
		}
		log.Debugf("sleeping for 1 second waiting for device %s to appear active", serialnumber)
		time.Sleep(time.Second * 1)
	}
	if devices == nil || len(devices) == 0 {
		return nil, fmt.Errorf("Device with serialnumber: " + serialnumber + " not found")
	}
	var isFound bool
	isFound = false
	for _, device := range devices {
		if device.SerialNumber == serialnumber && len(device.Slaves) > 0 {
			isFound = true
			dev = device
			log.Debugf("Device found %+v, now creating a filesystem on the device", dev)
			if device.Major == "" || device.Minor == "" {
				return nil, fmt.Errorf("device is not completely setup to create filesystem %+v", device)
			}
			setAltFullPathName(dev)
			if dev.AltFullPathName == "" {
				return nil, fmt.Errorf("device %v does not have full path set", dev)
			}
			err := RetryCreateFileSystem(device.AltFullPathName, fileSystemType)
			if err != nil {
				return nil, err
			}
			return dev, nil
		}
	}
	if isFound == false {
		return nil, fmt.Errorf("device with serialnumber: %s not found", serialnumber)
	}

	return dev, nil
}

// SetFilesystemOptions applies the FS options on the filesystem of the device
func SetFilesystemOptions(devPath string, fsOpts *model.FilesystemOpts) error {
	log.Tracef(">>>>> SetFilesystemOptions, devPath: %s, fsOptions: %+v", devPath, fsOpts)
	defer log.Trace("<<<<< SetFilesystemOptions")

	// Change Mode if requested
	if fsOpts.Mode != "" {
		mode := fsOpts.Mode
		log.Trace("Processing FS Mode: ", mode)
		if err := ChangeMode(devPath, mode); err != nil {
			return fmt.Errorf("unable to update the filesystem mode for device path %s to %s, err: %s", devPath, mode, err.Error())
		}
	}

	// Change Owner if requested
	if fsOpts.Owner != "" {
		owner := fsOpts.Owner
		log.Trace("Processing FS Owner: ", owner)
		userGroup := strings.Split(owner, ":")
		if len(userGroup) > 1 {
			if err := ChangeOwner(devPath, userGroup[0], userGroup[1]); err != nil {
				return fmt.Errorf("unable to change filesystem ownership to %v for device path %s, err: %s", userGroup, devPath, err.Error())
			}
		} else {
			log.Trace("No user group found for the filesystem owner ", userGroup)
		}
	}
	return nil
}

// RetryCreateFileSystem : retry file system create
func RetryCreateFileSystem(devPath string, fsType string) (err error) {
	log.Tracef(">>>>> RetryCreateFileSystem, devPath: %s, fsType: %s", devPath, fsType)
	defer log.Trace("<<<<< RetryCreateFileSystem")

	return RetryCreateFileSystemWithOptions(devPath, fsType, nil)
}

// RetryCreateFileSystemWithOptions : retry file system create
func RetryCreateFileSystemWithOptions(devPath string, fsType string, options []string) (err error) {
	log.Tracef(">>>>> RetryCreateFileSystemWithOptions, devPath: %s, fsType: %s, options: %v", devPath, fsType, options)
	defer log.Trace("<<<<< RetryCreateFileSystemWithOptions")

	maxTries := 8 //sleep for try second(s) on each try (eg. 1 + 2 + 3 + 4 + 5 + 6 + 7 + 8 ~= 36 seconds)
	try := 0
	for {
		err := CreateFileSystemWithOptions(devPath, fsType, options)
		log.Tracef("RetryCreateFileSystemWithOptions error=%v", err)
		if err != nil && (strings.Contains(err.Error(), noFileOrDirErr) || strings.Contains(err.Error(), "busy")) {
			if try < maxTries {
				try++
				log.Debugf("RetryCreateFileSystemWithOptions try=%d", try)
				time.Sleep(time.Duration(try) * time.Second)
				continue
			}
			return err
		}
		if err != nil {
			// if there are any generic errors do not retry
			return err
		}
		return nil
	}
}

// CreateFileSystem : creates file system on the device
func CreateFileSystem(devPath string, fsType string) (err error) {
	log.Tracef("createFileSystem called with %s %s", fsType, devPath)
	options := []string{devPath}
	return createFileSystem(fsType, options)
}

// CreateFileSystemWithOptions : creates file system on the device with creation options
func CreateFileSystemWithOptions(devPath string, fsType string, options []string) (err error) {
	log.Tracef("CreateFileSystemWithOptions called with %s %s %v", fsType, devPath, options)
	options = append(options, devPath)
	return createFileSystem(fsType, options)
}

func createFileSystem(fsType string, options []string) (err error) {
	var output string
	if fsType == FsType.String(Xfs) {
		output, _, err = util.ExecCommandOutputWithTimeout(fsxfscommand, options, defaultFSCreateTimeout)
	} else if fsType == FsType.String(Ext3) {
		output, _, err = util.ExecCommandOutputWithTimeout(fsext3command, options, defaultFSCreateTimeout)
	} else if fsType == FsType.String(Ext4) {
		output, _, err = util.ExecCommandOutputWithTimeout(fsext4command, options, defaultFSCreateTimeout)
	} else if fsType == FsType.String(Ext2) {
		output, _, err = util.ExecCommandOutputWithTimeout(fsext2command, options, defaultFSCreateTimeout)
	} else if fsType == FsType.String(Btrfs) {
		output, _, err = util.ExecCommandOutputWithTimeout(fsbtrfscommand, options, defaultFSCreateTimeout)
	} else {
		return fmt.Errorf("%s filesystem is unsupported", fsType)
	}
	if err != nil {
		return fmt.Errorf("unable to create filesystem: %s with args %s. Error: %s", fsType, options, err.Error())
	}
	if output == "" {
		return fmt.Errorf("filesystem not created using %v", options)
	}
	return nil
}

//UnmountFileSystem : unmount the filesystem
//nolint: gocyclo
func UnmountFileSystem(mountPoint string) (*model.Mount, error) {
	log.Tracef("UnmountFileSystem called with %s", mountPoint)
	if mountPoint == "" {
		return nil, fmt.Errorf("empty mountpoint was passed")
	}
	isFileExist, _, _ := util.FileExists(mountPoint)
	if !isFileExist {
		return nil, fmt.Errorf("mountpoint %s does not exist", mountPoint)
	}

	mountedDevice, err := GetDeviceFromMountPoint(mountPoint)
	if err != nil {
		return nil, fmt.Errorf("unable to get mounted Device from Mountpoint %s, err: %s", mountPoint, err.Error())
	}

	if mountedDevice == "" {
		log.Infof("No device is mounted on the mountpoint %s", mountPoint)
		return nil, nil
	}

	// try to unmount
	err = unmount(mountPoint)
	if err != nil {
		log.Errorf("unmount failed with path %s err=%s", mountPoint, err.Error())
		return nil, err
	}

	//verify unmount worked
	err = verifyUnMount(mountPoint)
	if err != nil {
		return nil, err
	}
	if mountedDevice != "" {
		log.Tracef("performing flushbufs on %+v", mountedDevice)
		dev := &model.Device{AltFullPathName: mountedDevice}
		err = flushbufs(dev)
		// best effort to flush the buffer
		if err != nil {
			log.Errorf(err.Error())
		}
	}

	// delete the mount point too
	log.Debugf("deleting the mountpoint %s", mountPoint)
	err = os.RemoveAll(mountPoint)
	if err != nil {
		log.Errorf("error to remove mountpoint %s : %s", mountPoint, err.Error())
		for i := 1; i <= 5; i++ {
			log.Debugf("attempt %d for removing mountpoint %s", i, mountPoint)
			err := os.RemoveAll(mountPoint)
			time.Sleep(1 * time.Second)
			if err == nil {
				break
			}
		}
		//best effort to remove the stale directory if the unmount was clean
		log.Debugf("Stat() may have failed on %s. Forcibly removing mountpoint (rm -rf).", mountPoint)
		args := []string{"-r", "-f", mountPoint}
		_, _, _ = util.ExecCommandOutput("rm", args)
	}

	mnt := &model.Mount{
		Mountpoint: mountPoint,
	}
	return mnt, nil
}

//UnmountDevice : unmount device from host
func UnmountDevice(device *model.Device, mountPoint string) (*model.Mount, error) {
	log.Trace("UnmountDevice called")
	if device == nil || mountPoint == "" {
		return nil, fmt.Errorf("no device or mountPoint present to unmount")
	}
	mount, err := UnmountFileSystem(mountPoint)
	return mount, err
}

// RemountWithOptions : Remount mountpoint with options
//nolint : dupl
func RemountWithOptions(mountPoint string, options []string) error {
	log.Tracef(">>>>> ReMountWithOptions, mountPoint: %s, options: %v", mountPoint, options)
	defer log.Trace("<<<<< ReMountWithOptions")

	if mountPoint == "" || len(options) == 0 {
		return fmt.Errorf("No mount point or mount options are present to remount")
	}

	reMountOptions := append([]string{"remount"}, options...)
	args := append([]string{"-o"}, strings.Join(reMountOptions, ","))
	args = append(args, mountPoint)
	try := 0
	var rc int
	var err error
	for {
		_, rc, err = util.ExecCommandOutput(mountCommand, args)
		if err != nil || rc != 0 {
			// if error is not nil for 5 retries
			if try < 5 {
				try++
				log.Tracef("re-mount not yet complete with err: %s, rc: %d.. will retry", err.Error(), rc)
				time.Sleep(time.Duration(try) * time.Second)
				continue
			}
		}
		break
	}
	if err != nil {
		if rc == mountUUIDErr {
			// TODO: this works for docker workflow. Need to see if it needs to be changed for oracle and other linux use cases
			log.Trace("rc=" + strconv.Itoa(mountUUIDErr) + " trying again with no uuid option")
			_, _, err = util.ExecCommandOutput(mountCommand, []string{"-o", "nouuid", mountPoint})
		}
	}
	if err != nil {
		log.Errorf("Failed to remount mountpoint %s, err: %v", mountPoint, err.Error())
		return err
	}
	return nil
}

// MountDevice : mount device on host
func MountDevice(device *model.Device, mountPoint string, options []string) (*model.Mount, error) {
	log.Tracef(">>>>> MountDevice, Device: %+v", device)
	defer log.Trace("<<<<< MountDevice")

	if device == nil || mountPoint == "" {
		return nil, fmt.Errorf("no device or mountPoint present to mount")
	}

	// Create mount directory if not already exists
	if _, err := CreateMountDir(mountPoint); err != nil {
		log.Errorf("Failed to create mount point %s, Err: %s", mountPoint, err.Error())
		return nil, err
	}

	// Mount the device at the mountPoint
	devPath := device.AltFullPathName
	if device.AltFullLuksPathName != "" {
		devPath = device.AltFullLuksPathName
	}
	mount, err := MountDeviceWithFileSystem(devPath, mountPoint, options)
	if mount == nil || err != nil {
		return nil, fmt.Errorf("unable to mount the device at mountPoint : %s. Error: %s", mountPoint, err.Error())
	}
	log.Tracef("Device %s was mounted at %s successfully", devPath, mountPoint)

	// Get the mount options
	mount.Options, err = GetMountOptions(devPath, mountPoint)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving the mount options for mountPoint %s, Err: %s", mountPoint, err.Error())
	}
	return mount, nil
}

// MountDeviceWithFileSystem : Mount device with filesystem at the mountPoint, if partitions are present, then
// largest partition will be mounted
func MountDeviceWithFileSystem(devPath string, mountPoint string, options []string) (*model.Mount, error) {
	log.Tracef(">>>>> MountDeviceWithFileSystem, devPath: %s, mountPoint: %s, options: %v", devPath, mountPoint, options)
	defer log.Trace("<<<<< MountDeviceWithFileSystem")

	if devPath == "" || mountPoint == "" {
		return nil, fmt.Errorf("devPath :%s or mountPoint %s cannot be empty", devPath, mountPoint)
	}

	// check if mountpoint already has any device
	mountedDevice, _ := GetDeviceFromMountPoint(mountPoint)
	if mountedDevice != "" {
		if mountedDevice != devPath {
			err := fmt.Errorf("%s is already mounted at %s. Skipping mount for %s", mountedDevice, mountPoint, devPath)
			log.Warnf(err.Error())
			return nil, err
		}
		if len(options) != 0 {
			// TODO: Remount with new options???
			log.Infof("Options %v are not applied for the existing mountPoint", options)
		}
		// TODO: Return mountOptions in the response.

		// if existing mount point present, return successful mount
		return &model.Mount{Mountpoint: mountPoint, Device: &model.Device{AltFullPathName: mountedDevice}}, nil
	}
	// check if the device already has filesystem on it
	fsType, err := GetFilesystemType(devPath)
	if err != nil {
		log.Errorf("unable to verify the filesystem type from device %s", devPath)
		return nil, err
	}

	if fsType == "" {
		return nil, fmt.Errorf("no filesystem type present on device %s. Failing mount ", devPath)
	}

	log.Tracef("got Filesystem Type=%s from device %s. Proceeding with mount.", fsType, devPath)

	// If there are partitions on the device, then mount the largest partition.
	deviceParitionInfos, err := GetPartitionInfo(&model.Device{AltFullPathName: devPath})
	if err != nil {
		return nil, fmt.Errorf("Failed to verify if partitions exist on device %s, %s", devPath, err.Error())
	}
	if len(deviceParitionInfos) != 0 {
		mount, err := mountForPartition(devPath, mountPoint, options)
		if mount != nil {
			return mount, nil
		}
		return nil, err
	}

	// whole block device. perform mount if not already mounted
	mount, err := performMount(devPath, mountPoint, options)
	if err != nil {
		return nil, err
	}
	return mount, nil
}

func MountNFSShare(source string, targetPath string, options []string, nfsType string) error {
	log.Tracef(">>>>> MountNFSShare called with source %s target %s type %s", source, targetPath, nfsType)
	defer log.Tracef("<<<<< MountNFSShare")

	// default type as nfs4
	if nfsType == "" {
		nfsType = defaultNFSType
	}

	mountedSource, _ := GetDeviceFromMountPoint(targetPath)
	if mountedSource != "" {
		// the source exists for the target path but differs from the expected mount, return error
		if mountedSource != source {
			err := fmt.Errorf("%s is already mounted at %s. Skipping mount for %s", mountedSource, source, targetPath)
			return err
		}
		// if mount point present with expected targetPath, return successful
		return nil
	}

	args := []string{fmt.Sprintf("-t%s", nfsType), source, targetPath}
	optionArgs := []string{}
	if len(options) != 0 {
		optionArgs = append([]string{"-o"}, strings.Join(options, ","))
	}
	args = append(optionArgs, args...)
	_, _, err := util.ExecCommandOutput(mountCommand, args)
	if err != nil {
		return err
	}
	// verify that mount is successful
	err = verifyMount(source, targetPath)
	if err != nil {
		return err
	}
	return nil
}

func performMount(devPath string, mountPoint string, options []string) (*model.Mount, error) {
	args := []string{devPath, mountPoint}
	optionArgs := []string{}
	if len(options) != 0 {
		optionArgs = append([]string{"-o"}, strings.Join(options, ","))
	}
	args = append(optionArgs, args...)
	try := 0
	var rc int
	var err error
	for {
		_, rc, err = util.ExecCommandOutput(mountCommand, args)
		if err != nil || rc != 0 {
			// if failed due to duplicate FS UUID(snapshot), attempt mount with no-uuid check option
			if rc == mountUUIDErr {
				log.Infof("mount failed for dev %s with rc=%d(duplicate uuid), trying again with no uuid option", devPath, mountUUIDErr)
				_, _, err = util.ExecCommandOutput(mountCommand, []string{"-o", "nouuid", devPath, mountPoint})
				if err != nil {
					return nil, err
				}
			} else if try < 5 {
				// retry on other generic errors
				try++
				log.Debugf("mount not yet complete with err :%s rc %d.. will retry", err.Error(), rc)
				time.Sleep(time.Duration(try) * time.Second)
				continue
			}
		}
		break
	}
	if err != nil {
		return nil, err
	}

	// verify that mount is successful
	err = verifyMount(devPath, mountPoint)
	if err != nil {
		return nil, err
	}
	return &model.Mount{Mountpoint: mountPoint, Device: &model.Device{AltFullPathName: devPath}}, nil
}

// GetFilesystemType returns the filesystem type if present else empty string
func GetFilesystemType(devPath string) (string, error) {
	log.Trace(">>>>> GetFilesystemType, devPath: ", devPath)
	defer log.Trace("<<<<< GetFilesystemType")

	// If there are partitions on the device, then return FS type of largest partition.
	deviceParitionInfos, err := GetPartitionInfo(&model.Device{AltFullPathName: devPath})
	if err != nil {
		return "", fmt.Errorf("Failed to verify if partitions exists on device %s, %s", devPath, err.Error())
	}
	if len(deviceParitionInfos) != 0 {
		largestPartition := getLargestPartition(deviceParitionInfos)
		if largestPartition != nil {
			log.Debugf("Got largest partition for device %s as %s to check fsType", devPath, largestPartition.Name)
			devPath = devMapperPath + largestPartition.Name
		}
	}

	// Sample input/output format:
	// # blkid dev/mapper/21bab810d4d816c6a6c9ce900b13eb9ef
	// dev/mapper/21bab810d4d816c6a6c9ce900b13eb9ef: UUID="63a91d01-b388-45fd-8ae3-ebe3b687200d" TYPE="xfs"
	args := []string{devPath}
	out, _, err := util.ExecCommandOutput(blkid, args)
	// blkid can fail with no output if there is no filesystem on the device yet. so treat that as no FS on device.
	if err != nil && len(out) != 0 {
		return "", fmt.Errorf("Failed to verify if FS exists on device %s, %s", devPath, err.Error())
	}
	// TODO: Add RegEx validation
	if len(out) != 0 {
		// split using space as delimiter
		list := strings.Split(out, " ")
		for _, item := range list {
			if strings.HasPrefix(item, "TYPE=") {
				strs := strings.Split(item, "=")
				if len(strs) >= 2 {
					value := strs[1]
					// Remove the newline chars if present
					value = strings.Trim(value, "\n")
					// Remove double quotes
					fsType := strings.Trim(value, "\"")
					log.Trace("Found filesystem type: ", fsType)
					return fsType, nil
				}
			}
		}
	}
	log.Trace("No filesystem found on the device ", devPath)
	return "", nil
}

func mountForPartition(devPath, mountPoint string, options []string) (mount *model.Mount, err error) {
	log.Tracef("mountForPartition called for %s and %s", devPath, mountPoint)
	// check if there are partitions
	mount = nil
	deviceParitionInfos, _ := GetPartitionInfo(&model.Device{AltFullPathName: devPath})
	if len(deviceParitionInfos) != 0 {
		largestPartition := getLargestPartition(deviceParitionInfos)
		if largestPartition != nil {
			log.Tracef("%s could not be mounted, but partition(s) were found %v", devPath, largestPartition)
			// lsblk talks in terms of dev mapper
			mount, err = performMount(devMapperPath+largestPartition.Name, mountPoint, options)
			if err != nil {
				return nil, err
			}
			log.Tracef("mounted using partition %s", devMapperPath+largestPartition.Name)
		}
	}
	return mount, nil
}

func verifyUnMount(mountPoint string) error {
	mountedDevice, err := GetDeviceFromMountPoint(mountPoint)
	if err != nil {
		return err
	}
	if mountedDevice != "" {
		return fmt.Errorf("unsuccessful in attempt to unmount. %s  is still mounted at %s", mountedDevice, mountPoint)
	}

	log.Tracef("%s is unmounted", mountPoint)
	return nil
}

func verifyMount(devPath, mountPoint string) error {
	mountedDevice, err := GetDeviceFromMountPoint(mountPoint)
	if err != nil {
		return err
	}
	if mountedDevice == "" {
		return fmt.Errorf("device %s is not mounted at %s", devPath, mountPoint)
	}
	log.Tracef("%s is mounted at %s", devPath, mountPoint)
	return nil
}

// GetFsType returns filesytem type for a given mount object
func GetFsType(mount model.Mount) (fsType string, err error) {
	log.Trace("GetFsType called with device ", mount.Device.AltFullPathName)
	mountLines, err := util.FileGetStrings(procMounts)
	if err != nil {
		return "", err
	}
	for _, line := range mountLines {
		entry := strings.Fields(line)
		if len(entry) > 4 {
			if strings.TrimSpace(entry[0]) == mount.Device.AltFullPathName {
				return entry[2], nil
			}
		}
	}
	return "", fmt.Errorf("device %s is not mounted", mount.Device.AltFullPathName)
}

// ExcludeMountDirFromUpdateDb add mountDir to PRUNEPATHS in updatedb.conf to let mlocate ignore mountDir during file search
func ExcludeMountDirFromUpdateDb(mountDir string) (err error) {
	// ignore error here as file can be missing if mlocate is not installed and we don't need to update in that case
	exists, _, _ := util.FileExists(updateDbConf)
	if !exists {
		return nil
	}
	lines, _ := util.FileGetStrings(updateDbConf)
	lines, isChanged := updateDbConfiguration(lines, mountDir)
	if !isChanged {
		return nil
	}

	err = util.FileWriteStrings(updateDbConf, lines)
	if err != nil {
		log.Errorln("unable to add our mount directory to PRUNEPATHS", err.Error())
		return err
	}
	return nil
}

func updateDbConfiguration(lines []string, mountDir string) (updatedLines []string, isChanged bool) {
	found := false
	r := regexp.MustCompile(prunePathsPattern)
	for index, line := range lines {
		if !r.MatchString(line) {
			continue
		}
		pathsMatch := util.FindStringSubmatchMap(line, r)
		if val, ok := pathsMatch["paths"]; ok {
			paths := strings.Fields(val)
			// loop through each path and identify if our mount directory is included already
			for _, path := range paths {
				if !strings.EqualFold(strings.TrimSpace(path), mountDir) {
					continue
				}
				log.Traceln("PRUNEPATHS already contains our mount directory")
				found = true
				break
			}
			if !found {
				// add our mount directory to ignore during file locate
				paths = append(paths, mountDir)
				lines[index] = "PRUNEPATHS=\"" + strings.Join(paths, " ") + "\""
				return lines, true
			}
			break
		}
	}
	return lines, false
}

// CheckFsCreationInProgress checks if mkfs process is using the device using lsof
func CheckFsCreationInProgress(device model.Device) (inProgress bool, err error) {
	args := []string{device.AltFullPathName}
	out, _, err := util.ExecCommandOutput(lsof, args)
	if err != nil {
		return false, fmt.Errorf("failed to verify if FS creation is in progress on device %s", err.Error())
	}
	if strings.Contains(out, "mkfs") {
		return true, nil
	}
	return false, nil
}

// CreateMountDir creates new directory if not exists
func CreateMountDir(mountPoint string) (isDir bool, err error) {
	log.Tracef(">>>>> CreateMountDir, mountPoint: %s", mountPoint)
	defer log.Trace("<<<<< CreateMountDir")

	err = os.MkdirAll(mountPoint, 700)
	// if previous mount has not got cleaned up the mountPoint may already exist. Don't treat it as an error
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "file exists") {
		return false, err
	}
	_, isDir, _ = util.FileExists(mountPoint)
	if isDir == false {
		log.Error("Failed to created mountpoint" + mountPoint)
		return false, errors.New("failed to create directory at mountpoint " + mountPoint)
	}
	return isDir, nil
}

// getUserID returns the uid for the given user name.
func getUserID(userName string) (string, error) {
	usr, err := user.Lookup(userName)
	if err != nil {
		log.Errorf("Failed to lookup user %s, err: %v", userName, err.Error())
		return "", err
	}
	return usr.Uid, nil
}

// getGroupID returns the gid for the given group name.
func getGroupID(groupName string) (string, error) {
	grp, err := user.LookupGroup(groupName)
	if err != nil {
		log.Errorf("Failed to lookup group %s, err: %v", groupName, err.Error())
		return "", err
	}
	return grp.Gid, nil
}

// IsFsOwnerMatch checks if the fsOwner matches with the existing FsOwner info
func IsFsOwnerMatch(fsOwner string, existingFsOwner string) (bool, error) {
	log.Tracef(">>>>> isFsOwnerMatch, fsOwner: %s, existingFsOwner: %s", fsOwner, existingFsOwner)
	defer log.Trace("<<<<< isFsOwnerMatch")

	// The fsOwner might contain username and groupname instead of uid and gid integers.
	// So, get their corresponding userId/groupId and compare with the existingFsOwner (uid:gid)
	userGroupList := strings.Split(fsOwner, ":")
	usrStr := userGroupList[0]
	grpStr := userGroupList[1]

	// Check if user is INT. If yes, then use it as uid
	_, err := strconv.Atoi(usrStr)
	if err != nil { // string
		usrStr, err = getUserID(usrStr)
		if err != nil {
			return false, err
		}
	}

	// Check if group is INT. If yes ,then use it as gid
	_, err = strconv.Atoi(grpStr)
	if err != nil { // string
		grpStr, err = getGroupID(grpStr)
		if err != nil {
			return false, err
		}
	}

	// The existingFsOwner will always contain uid and gid integers
	existingUsrGrpIDList := strings.Split(existingFsOwner, ":")
	existingUsrID := existingUsrGrpIDList[0]
	existingGrpID := existingUsrGrpIDList[1]
	log.Tracef("Attempting to match %s:%s v/s %s:%s", usrStr, grpStr, existingUsrID, existingGrpID)

	return (strings.Compare(usrStr, existingUsrID) == 0 && strings.Compare(grpStr, existingGrpID) == 0), nil
}

// SetupFilesystem writes the given filesystem on the given device.
// If the requested FS already exists on the device, then it returns success.
func SetupFilesystem(device *model.Device, filesystemType string) error {
	log.Tracef(">>>>> SetupFilesystem, device: %+v, type: %s", device, filesystemType)
	defer log.Trace("<<<<< SetupFilesystem")

	if filesystemType == "" {
		return fmt.Errorf("Missing filesystem type")
	}

	devPath := device.AltFullPathName
	if device.AltFullLuksPathName != "" {
		devPath = device.AltFullLuksPathName
	}

	// Check if the device has filesystem already
	fsType, err := GetFilesystemType(devPath)
	if err != nil {
		return err
	}
	if fsType != "" {
		log.Tracef("Filesystem %s already exists on the device %s", fsType, devPath)
		return nil
	}

	log.Tracef("Creating filesystem %s on device path %s", filesystemType, devPath)
	if err := RetryCreateFileSystem(devPath, filesystemType); err != nil {
		log.Errorf("Failed to create filesystem %s on device with path %s", filesystemType, devPath)
		return err
	}
	return nil
}

// SetupFilesystemWithOptions writes the given filesystem on the given device using the given options.
// If the requested FS already exists on the device, then it returns success.
func SetupFilesystemWithOptions(device *model.Device, filesystemType string, options []string) error {
	log.Tracef(">>>>> SetupFilesystemWithOptions, device: %+v, type: %s, options: %v", device, filesystemType, options)
	defer log.Trace("<<<<< SetupFilesystemWithOptions")

	if filesystemType == "" {
		return fmt.Errorf("Missing filesystem type")
	}

	// Check if the device has filesystem already
	fsType, err := GetFilesystemType(device.AltFullPathName)
	if err != nil {
		return err
	}
	if fsType != "" {
		log.Tracef("Filesystem %s already exists on the device %s", fsType, device.AltFullPathName)
		return nil
	}

	log.Tracef("Creating filesystem %s on device path %s", filesystemType, device.AltFullPathName)
	if err := RetryCreateFileSystemWithOptions(device.AltFullPathName, filesystemType, options); err != nil {
		log.Errorf("Failed to create filesystem %s on device with path %s", filesystemType, device.AltFullPathName)
		return err
	}
	return nil
}

// SetupMount creates the mountpoint with mount options
func SetupMount(device *model.Device, mountPoint string, mountOptions []string) (*model.Mount, error) {
	log.Tracef(">>>>> SetupMount, device: %+v, mountPoint: %s, mountOptions: %v", device, mountPoint, mountOptions)
	defer log.Trace("<<<<< SetupMount")

	devPath := device.AltFullPathName
	if device.AltFullLuksPathName != "" {
		devPath = device.AltFullLuksPathName
	}

	// Mount device
	mount, err := MountDevice(device, mountPoint, mountOptions)
	if err != nil {
		return nil, fmt.Errorf("Error mounting device %s on mountpoint %s with options %v, %v", device.AltFullPathName, mountPoint, mountOptions, err.Error())
	}
	if mount == nil {
		return nil, fmt.Errorf("Unable to find the mounted device %s", device.SerialNumber)
	}
	log.Tracef("Device %+v mounted on %s successfully", mount.Device, mount.Mountpoint)

	if len(mountOptions) != 0 {
		// Check if the requested mount options are already compatible (i.e, set already)
		if !stringformat.StringsLookup(mount.Options, mountOptions) {
			// Apply mount options
			// TODO: Encryption would require devPath to be passed instead
			//       Although this won't have any impact except a log line where
			//       AltFullPathName gets printed
			if err = SetMountOptions(devPath, mountPoint, mountOptions); err != nil {
				return nil, fmt.Errorf("Error applying mount options %v on the mountpoint %s, %v", mountOptions, mountPoint, err.Error())
			}
			// Get the updated mount options
			updatedMountOptions, err := GetMountOptions(devPath, mountPoint)
			if err != nil {
				return nil, fmt.Errorf("Error retrieving mount options for the mountpoint %s, %v", mountPoint, err.Error())
			}
			// Update mount options
			mount.Options = updatedMountOptions
		}
	}
	return mount, nil
}

// AreFsOptionsCompatible returns true if the FS options are compatible
func AreFsOptionsCompatible(fsOpts *model.FilesystemOpts, existingFsOptions *model.FilesystemOpts) (bool, error) {
	if fsOpts == nil || existingFsOptions == nil {
		return false, fmt.Errorf("Invalid input specified. One of the inputs are nil")
	}
	if fsOpts.Type != existingFsOptions.Type {
		return false, fmt.Errorf("Mismatch in the filesystem type. Got %s, expected %s", existingFsOptions.Type, fsOpts.Type)
	}
	if fsOpts.Mode != existingFsOptions.Mode {
		return false, fmt.Errorf("Mismatch in the filesystem mode. Got %s, expected %s", existingFsOptions.Mode, fsOpts.Mode)
	}
	if fsOpts.Owner != existingFsOptions.Owner {
		// Input FsOwner might contain username/groupname. So, decode them first and then try comparing.
		match, err := IsFsOwnerMatch(fsOpts.Owner, existingFsOptions.Owner)
		if err != nil {
			return false, fmt.Errorf("Error while mismatching the filesystem owner %s with %s, %v", existingFsOptions.Owner, fsOpts.Owner, err.Error())
		}
		if !match {
			return false, fmt.Errorf("Mismatch in the filesystem owner. Got %s, expected %s", existingFsOptions.Owner, fsOpts.Owner)
		}
	}
	return true, nil
}

// ExpandFilesystem : expands filesystem size to underlying device size
func ExpandFilesystem(devPath, mountPath, fsType string) (err error) {
	log.Tracef(">>>>> ExpandFilesystem called with dev %s mount %s fs %s", devPath, mountPath, fsType)
	defer log.Traceln("<<<<< ExpandFilesystem")

	switch fsType {
	case FsType.String(Xfs):
		_, _, err = util.ExecCommandOutputWithTimeout("xfs_growfs", []string{mountPath}, defaultFSCreateTimeout)
	case FsType.String(Ext2):
		fallthrough
	case FsType.String(Ext3):
		fallthrough
	case FsType.String(Ext4):
		_, _, err = util.ExecCommandOutputWithTimeout("resize2fs", []string{devPath}, defaultFSCreateTimeout)
	case FsType.String(Btrfs):
		_, _, err = util.ExecCommandOutputWithTimeout("btrfs", []string{"filesystem", "resize", "max", mountPath}, defaultFSCreateTimeout)
	default:
		err = fmt.Errorf("unsupported filesystem %s for online expand on dev %s mount %s", fsType, devPath, mountPath)
	}
	return err
}

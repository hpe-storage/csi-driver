// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"os"
	"syscall"
)

// GetFilesystemOptions applies the FS options on the filesystem of the device
func GetFilesystemOptions(device *model.Device, mountPoint string) (*model.FilesystemOpts, error) {
	log.Tracef(">>>>> GetFilesystemOptions, device: %+v, mountPoint: %s", device, mountPoint)
	defer log.Trace("<<<<< GetFilesystemOptions")

	// Get FS type
	fsType, err := GetFilesystemType(device.AltFullPathName)
	if err != nil {
		return nil, err
	}
	// No filesystem found on the device, return nil
	if fsType == "" {
		return nil, fmt.Errorf("No filesystem found on the device %s", device.AltFullPathName)
	}
	fsOpts := &model.FilesystemOpts{
		Type: fsType,
	}

	// Get Uid and Gid if mountpoint is specified
	if mountPoint != "" {
		// Get file info
		fileInfo, err := os.Stat(mountPoint)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving file stat for %s", mountPoint)
		}
		// Get Uid and Gid
		stat, ok := fileInfo.Sys().(*syscall.Stat_t)
		if !ok {
			log.Error("Not a syscall.Stat_t")
			return nil, fmt.Errorf("Error retrieving Uid and Gid for %s", mountPoint)
		}
		log.Tracef("stat = %#v", stat)
		fsOpts.Mode = fmt.Sprintf("%o", fileInfo.Mode().Perm())
		fsOpts.Owner = fmt.Sprintf("%v:%v", stat.Uid, stat.Gid)
	}
	return fsOpts, nil
}

// TODO: Move other Linux FS specific functions from 'mount.go' to 'filesystem_linux.go'

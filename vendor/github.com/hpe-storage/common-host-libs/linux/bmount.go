/*
(c) Copyright 2017 Hewlett Packard Enterprise Development LP

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package linux

import (
	"fmt"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
	"strings"
	"sync"
	"time"
)

const (
	mountCommand  = "mount"
	umountCommand = "umount"
)

var (
	mountMutex sync.Mutex
)

func unmount(mountPoint string) error {
	// try to unmount
	args := []string{mountPoint}
	_, rc, err := util.ExecCommandOutput(umountCommand, args)
	if err != nil || rc != 0 {
		if strings.Contains(err.Error(), "kill") {
			log.Warnf("ignore kill as an error for unmount")
			return nil
		}
		log.Errorf("unable to unmount %s with err=%s and rc=%d", mountPoint, err.Error(), rc)
		return err
	}
	return nil
}

// RetryBindMount :
func RetryBindMount(path, mountPoint string, rbind bool) error {
	log.Tracef("RetryBindMount called with %s %s %v", path, mountPoint, rbind)
	maxTries := 3
	try := 0
	for {
		err := BindMount(path, mountPoint, rbind)
		if err != nil {
			log.Errorf("BindMount failed for path %s, mountPoint %s and rbind %v : %s", path, mountPoint, rbind, err.Error())
			if try < maxTries {
				try++
				log.Tracef("RetryBindMount try=%d", try)
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

//BindMount mounts a path to a mountPoint. The rbind flag controls recursive binding.
func BindMount(path, mountPoint string, rbind bool) error {
	log.Tracef("BindMount called with %s %s %v", path, mountPoint, rbind)
	flag := "--bind"
	if rbind {
		flag = "--rbind"
	}

	args := []string{flag, path, mountPoint}
	out, rc, err := util.ExecCommandOutput(mountCommand, args)
	log.Tracef("output fom BindMount for path %s mountPoint %s, rbind %v is %s", out, mountPoint, rbind, out)

	if err != nil || rc != 0 {
		log.Errorf("BindMount failed with %d. It was called with %s %s %v. Output=%v.", rc, path, mountPoint, rbind, out)
		return err
	}

	return nil
}

// RetryBindUnmount :
func RetryBindUnmount(mountPoint string) error {
	log.Tracef("RetryBindUnmount called with %s", mountPoint)
	maxTries := 3
	try := 0
	for {
		err := BindUnmount(mountPoint)
		if err != nil {
			if try < maxTries {
				try++
				log.Tracef("RetryBindUnmount try=%d", try)
				time.Sleep(time.Duration(try) * time.Second)
				continue
			}
			log.Errorf("BindUnmount failed for mountpoint %s : %s", mountPoint, err.Error())
			return err
		}
		if err != nil {
			return err
		}
		return nil
	}
}

//BindUnmount unmounts a bind mount.
func BindUnmount(mountPoint string) error {
	log.Tracef("BindUnmount called with %s", mountPoint)

	mountedDevice, err := GetDeviceFromMountPoint(mountPoint)
	if err != nil {
		return fmt.Errorf("Unable to get mounted device from mountpoint %s, err: %s", mountPoint, err.Error())
	}

	if mountedDevice == "" {
		log.Infof("No device is mounted at mountpoint %s", mountPoint)
		return nil
	}

	err = unmount(mountPoint)
	if err != nil {
		return err
	}
	return nil
}

//GetDeviceFromMountPoint returns the device path from /proc/mounts
// for the mountpoint provided.  For example /dev/mapper/mpathd might be
// returned for /mnt.
func GetDeviceFromMountPoint(mountPoint string) (string, error) {
	log.Trace("getDeviceFromMountPoint called with ", mountPoint)
	return getMountsEntry(mountPoint, false)
}

//GetMountPointFromDevice returns the FIRST mountpoint listed in
// /proc/mounts matching the device.  Note that /proc/mounts lists
// device paths using the device mapper format.  For example: /dev/mapper/mpathd
func GetMountPointFromDevice(devPath string) (string, error) {
	log.Trace("getMountPointFromDevice called with ", devPath)
	return getMountsEntry(devPath, true)
}

func getMountsEntry(path string, dev bool) (string, error) {
	log.Tracef(">>>>> getMountsEntry called with path:%v isDev:%v", path, dev)
	defer log.Trace("<<<<< getMountsEntry")

	// take a lock on access mounts
	mountMutex.Lock()
	defer mountMutex.Unlock()

	var args []string
	out, _, err := util.ExecCommandOutput(mountCommand, args)
	if err != nil {
		return "", err
	}

	mountLines := strings.Split(out, "\n")
	log.Tracef("number of mounts retrieved %d", len(mountLines))

	var searchIndex, returnIndex int
	if dev {
		returnIndex = 2
	} else {
		searchIndex = 2
	}

	for _, line := range mountLines {
		entry := strings.Fields(line)
		log.Trace("mounts entry :", entry)
		if len(entry) > 3 {
			if entry[searchIndex] == path {
				log.Debugf("%s was found with %s", path, entry[returnIndex])
				return entry[returnIndex], nil
			}
		}
	}
	return "", nil
}

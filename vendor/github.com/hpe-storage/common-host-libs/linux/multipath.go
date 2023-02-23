// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
)

var (
	showPathsFormat      = []string{"show", "paths", "format", "%w %d %t %i %o %T %z %s %m"}
	showMapsFormat       = []string{"show", "maps", "format", "%w %d %n %s"}
	orphanPathRegexp     = regexp.MustCompile(getOrphanPathsPattern())
	multipathMutex       sync.Mutex
	deviceVendorPatterns = []string{"Nimble", "3PARdata", "TrueNAS", "FreeNAS"}
)

const (
	// MultipathConf configuration file for multipathd
	MultipathConf = "/etc/multipath.conf"
	// MultipathBindings bindings file for multipathd
	MultipathBindings  = "/etc/multipath/bindings"
	orphanPathsPattern = ".*\\s+(?P<host>\\d+):(?P<channel>\\d+):(?P<target>\\d+):(?P<lun>\\d+).*(REPLACE_VENDOR).*orphan"
	maxTries           = 3
)

func getOrphanPathsPattern() string {
	vendorPattern := strings.Join(deviceVendorPatterns, "|")
	return strings.Replace(orphanPathsPattern, "REPLACE_VENDOR", vendorPattern, -1)
}

// MultipathdShowMaps output
func MultipathdShowMaps(serialNumber string) (a []string, err error) {
	log.Tracef(">>>>> MultipathdShowMaps for %s", serialNumber)
	defer log.Trace("<<<<< MultipathdShowMaps")

	return multipathdShowCmd(showMapsFormat, serialNumber)
}

// MultipathdShowPaths output from Linux
func MultipathdShowPaths(serialNumber string) (a []string, err error) {
	log.Tracef(">>>>> MultipathdShowPaths for %s", serialNumber)
	defer log.Trace("<<<<< MultipathdShowPaths")

	return multipathdShowCmd(showPathsFormat, serialNumber)
}

// MultipathdReconfigure reconfigure multipathd settings
func MultipathdReconfigure() (out string, err error) {
	log.Trace(">>>>> MultipathdReconfigure")
	defer log.Trace("<<<<<< MultipathdReconfigure")

	multipathMutex.Lock()
	defer multipathMutex.Unlock()

	var args []string
	args = []string{"reconfigure"}
	out, _, err = util.ExecCommandOutput(multipathd, args)
	if err != nil {
		log.Error("unable to reconfigure multipathd settings", err.Error())
		err = fmt.Errorf("unable to reconfigure multipathd settings, Error: %s %s", err.Error(), out)
	}
	return out, err
}

func isMultipathTimeoutError(msg string) bool {
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "receiving packet")
}

func multipathShowCmdOrphanPaths() (output []string, err error) {
	log.Trace(">>>>> multipathShowCmdOrphanPaths")
	defer log.Trace("<<<<<< multipathShowCmdOrphanPaths")

	multipathMutex.Lock()
	defer multipathMutex.Unlock()

	out, _, err := util.ExecCommandOutput(multipathd, showPathsFormat)
	if err != nil {
		log.Warnf("multipathdShowCmd: error %v with args %v", err, showPathsFormat)
		return nil, err
	}
	// rc can be 0 on the below error conditions as well
	if isMultipathTimeoutError(out) {
		err = fmt.Errorf("failed to get multipathd %v, out %s", showPathsFormat, out)
		log.Warn(err.Error())
		return nil, err
	}
	// look for orphan paths
	listMultipathShowCmdOut := orphanPathRegexp.FindAllString(out, -1)
	return listMultipathShowCmdOut, nil
}

func multipathdShowCmd(args []string, serialNumber string) (output []string, err error) {
	multipathMutex.Lock()
	defer multipathMutex.Unlock()

	out, _, err := util.ExecCommandOutput(multipathd, args)
	if err != nil {
		log.Warnf("multipathdShowCmd: error %v with args %v", err, args)
		return nil, err
	}
	// rc can be 0 on the below error conditions as well
	if isMultipathTimeoutError(out) {
		err = fmt.Errorf("failed to get multipathd %v, out %s", args, out)
		log.Warn(err.Error())
		return nil, err
	}
	r, err := regexp.Compile("(?m)^.*" + serialNumber + ".*$")
	if err != nil {
		log.Errorf("unable to compile the regex %s", err.Error())
		return nil, err
	}
	// split on new lines'
	listMultipathShowCmdOut := r.FindAllString(out, -1)
	return listMultipathShowCmdOut, nil
}

// tearDownMultipathDevice : tear down the hiearchy of multipath device and scsi paths
func tearDownMultipathDevice(dev *model.Device) (err error) {
	log.Tracef(">>>>> tearDownMultipathDevice called for %s", dev.SerialNumber)
	defer log.Trace("<<<<< tearDownMultipathDevice")

	lines, err := MultipathdShowMaps(dev.SerialNumber)
	if err != nil {
		log.Trace(err)
		return err
	}
	for _, line := range lines {
		if strings.Contains(line, dev.SerialNumber) {
			entry := strings.Fields(line)
			if strings.Contains(entry[0], dev.SerialNumber) {
				// entry[0] --> uuid
				// entry[1] --> dm device name
				// entry[2] --> dm map name
				log.Debugf("uuid %s dm %s map name %s", entry[0], entry[1], entry[2])
				err := retryCleanupDeviceAndSlaves(dev)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkIfDeviceCanBeDeleted(dev *model.Device) (err error) {
	// first check if the device is already mounted
	var devices []*model.Device
	devices = append(devices, dev)
	mounts, err := GetMountPointsForDevices(devices)
	if err != nil {
		return err
	}
	if len(mounts) > 0 {
		log.Errorf("%s is currently mounted", dev.Pathname)
		return fmt.Errorf("%s is currently mounted", dev.Pathname)
	}
	// check if the device is part of LVM or other device mapper devices
	holder, err := getDeviceHolders(dev)
	if err != nil {
		return err
	}

	if holder != "" {
		is := isPartitionPresent(dev)
		if !is {
			log.Errorf("%s is used either by LVM or other dm Device", dev.Pathname)
			return fmt.Errorf("%s is used either by LVM or other dm Device", dev.Pathname)
		}
		err = cleanPartitions(dev)
		if err == nil {
			return nil
		}
	}
	return nil
}

// retry for maxtries for device Cleanup
func retryCleanupDeviceAndSlaves(dev *model.Device) error {
	log.Trace(">>>>> retryCleanupDeviceAndSlaves")
	defer log.Trace("<<<<< retryCleanupDeviceAndSlaves")

	// check if mpathName exists for the device, else populate it
	if dev.MpathName == "" {
		err := setAltFullPathName(dev)
		if err != nil {
			return err
		}
	}

	try := 0
	maxTries := 10 // retry for 50 seconds with periodic interval of 5 seconds
	for {
		err := cleanupDeviceAndSlaves(dev)
		if err != nil {
			if try < maxTries {
				try++
				log.Debugf("retryCleanupDeviceAndSlaves try=%d", try)
				time.Sleep(5 * time.Second)
				continue
			}
			return err
		}
		return nil
	}
}

// cleanupDeviceAndSlaves : remove the multipath devices and its slaves and logout iscsi targets
// nolint: gocyclo
func cleanupDeviceAndSlaves(dev *model.Device) (err error) {
	log.Tracef(">>>>> cleanupDeviceAndSlaves called for %+v", dev)
	defer log.Trace("<<<<< cleanupDeviceAndSlaves")

	isFC := isFibreChannelDevice(dev.Slaves)

	// disable queuing on multipath
	err = multipathDisableQueuing(dev)
	if err != nil {
		log.Error(err)
	}
	// remove dm device
	removeErr := multipathRemoveDmDevice(dev)
	if removeErr != nil {
		log.Error(removeErr.Error())
		// proceed with the rest of the path cleanup nevertheless, as mpath might get deleted asynchronously
	}

	var isGst = true
	log.Debugf("targetScope for device %s is \"%s\"", dev.AltFullPathName, dev.TargetScope)
	if dev.TargetScope == "" || strings.EqualFold(dev.TargetScope, VolumeScope.String()) {
		isGst = false
	}

	//delete all physical paths of the device
	if (!isFC && dev.IscsiTargets != nil && !isGst) {
		log.Debugf("volume scoped target %+v, initiating iscsi logout and delete", dev.IscsiTargets)
		err = logoutAndDeleteIscsiTarget(dev)
		if err != nil {
			log.Error(err.Error())
			return err
		}
	} else {
		// check for all paths again and make sure we have all paths for cleanup
		allPaths, err := multipathGetPathsOfDevice(dev, false)
		if err != nil {
			return err
		}
		// delete sd devices, only for GST types, as VST target logout will cleanup devices automatically
		// delete scsi devices
		err = deleteSdDevices(allPaths)
		if err != nil {
			return err
		}
	}

	// indicate if we were not able to cleanup multipath map, so the caller can retry
	if removeErr != nil {
		return removeErr
	}
	return nil
}

func deleteSdDevices(paths []*model.PathInfo) error {
	log.Tracef(">>>>> deleteSdDevices")
	defer log.Trace("<<<<< deleteSdDevices")

	for _, path := range paths {
		err := deleteSdDevice(path.Device)
		if err != nil {
			log.Tracef("failed to delete scsi device %s. Error: %s", path.Device, err.Error())
			// try to cleanup rest of the paths
			continue
		}
	}
	return nil
}

func logoutAndDeleteIscsiTarget(dev *model.Device) error {
	log.Tracef(">>>>> logoutAndDeleteIscsiTarget for device %s", dev.AltFullPathName)
	defer log.Tracef("<<<<< logoutAndDeleteIscsiTarget")

	if dev.IscsiTargets != nil {
		for _, iscsiTarget := range dev.IscsiTargets {
			log.Tracef("initiating the iscsi target logout for %s of type %s", iscsiTarget.Name, iscsiTarget.Scope)
			//logout of iscsi target
			err := iscsiLogoutOfTarget(iscsiTarget)
			if err != nil {
				return fmt.Errorf("unable to logout iscsi target %s. Error: %s", iscsiTarget, err.Error())
			}
			//delete iscsi node
			err = iscsiDeleteNode(iscsiTarget)
			if err != nil {
				return fmt.Errorf("unable to delete iscsi target: %s. Error: %s", iscsiTarget, err.Error())
			}
		}
	}
	return nil
}

// multipathDisableQueuing : disable queueing on the multipath device
func multipathDisableQueuing(dev *model.Device) (err error) {
	log.Trace(">>>>> multipathDisableQueuing for", dev.MpathName)
	defer log.Trace("<<<<< multipathDisableQueuing")

	multipathMutex.Lock()
	defer multipathMutex.Unlock()

	args := []string{"message", dev.MpathName, "0", "fail_if_no_path"}
	out, _, err := util.ExecCommandOutput(dmsetupcommand, args)
	if err != nil {
		return err
	}
	if out != "" && strings.Contains(out, deviceDoesNotExist) {
		return fmt.Errorf("failed to disable queuing for %s. Error: %s", dev.MpathName, out)
	}
	return nil
}

// multipathRemoveDmDevice : remove multipath device ps via dmsetup
func multipathRemoveDmDevice(dev *model.Device) (err error) {
	log.Tracef(">>>>> multipathRemoveDmDevice called for %+v", dev)
	defer log.Trace("<<<<< multipathRemoveDmDevice")

	multipathMutex.Lock()
	defer multipathMutex.Unlock()

	if dev.MpathName == "" {
		err = setAltFullPathName(dev)
		if err != nil {
			return err
		}
	}
	args := []string{"remove", "--force", dev.MpathName}
	out, _, err := util.ExecCommandOutput(dmsetupcommand, args)
	if err != nil {
		return fmt.Errorf("failed to remove multipath map for %s. Error: %s", dev.MpathName, err.Error())
	}
	if out != "" && !strings.Contains(out, "ok") && !strings.Contains(out, deviceDoesNotExist) {
		return fmt.Errorf("failed to remove device map for %s. Error: %s", dev.MpathName, out)
	}

	log.Debugf("successfully removed the dm device %s", dev.MpathName)
	return nil
}

func cleanupErrorMultipathMaps() (err error) {
	log.Traceln(">>> cleanupErrorMultipathMaps")
	defer log.Traceln("<<< cleanupErrorMultipathMaps")

	multipathMutex.Lock()
	defer multipathMutex.Unlock()

	// run dmsetup table ls and fetch error maps
	args := []string{"table"}
	out, _, err := util.ExecCommandOutput(dmsetupcommand, args)
	if err != nil {
		return err
	}
	// split on new lines
	listErrorMaps := errorMapRegex.FindAllString(out, -1)
	for _, errorMap := range listErrorMaps {
		result := util.FindStringSubmatchMap(errorMap, errorMapRegex)
		if mapName, ok := result["mapname"]; ok {
			args := []string{"remove", mapName}
			_, _, err := util.ExecCommandOutput(dmsetupcommand, args)
			if err != nil {
				// ignore errors and only log, as its a best effort to cleanup all error maps
				log.Debugf("unable to cleanup error state map %s err %s", mapName, err.Error())
				continue
			}
			log.Debugf("successfully cleaned error state map %s", mapName)
		}
	}
	return nil
}

func retryGetPathOfDevice(dev *model.Device, needActivePath bool) (paths []*model.PathInfo, err error) {
	log.Tracef("retryGetActivePathOfDevice called for device with uuid(%s) and needActivePath %v ", dev.SerialNumber, needActivePath)
	try := 0
	maxTries := 5
	for {
		paths, err := multipathGetPathsOfDevice(dev, needActivePath)
		if err != nil {
			if isMultipathTimeoutError(err.Error()) {
				if try < maxTries {
					try++
					time.Sleep(5 * time.Second)
					continue
				}
			}
			return nil, err
		}
		if needActivePath {
			if len(paths) > 0 {
				log.Tracef("active scsi paths successfully found for %s", dev.AltFullPathName)
			} else {
				// don't treat no scsi paths as an error
				log.Debugf("no scsi paths present for device %s", dev.AltFullPathName)
				return paths, nil
			}
		}
		return paths, nil
	}
}

func multipathGetOrphanPathsByLunID(lunID string) []string {
	log.Tracef(">>>> multipathGetOrphanPathsByLunID called with %s", lunID)
	defer log.Trace("<<<<< multipathGetOrphanPathsByLunID")
	var hctls []string
	lines, _ := multipathShowCmdOrphanPaths()
	if len(lines) == 0 {
		log.Tracef("no orphan paths found for lunID %s", lunID)
		return hctls
	}

	for _, line := range lines {
		result := util.FindStringSubmatchMap(line, orphanPathRegexp)
		lun := result["lun"]
		if lun == lunID {
			hctl := result["host"] + ":" + result["channel"] + ":" + result["target"] + ":" + lun
			log.Debugf("orphan h:c:t:l found is %s", hctl)
			hctls = append(hctls, hctl)
		}
	}
	return hctls
}

// multipathGetPathsOfDevice : get all scsi paths and host, channel information of multipath device
// nolint : gocyclo
func multipathGetPathsOfDevice(dev *model.Device, needActivePath bool) (paths []*model.PathInfo, err error) {
	log.Tracef(">>>> multipathGetPathsOfDevice with %s", dev.SerialNumber)
	defer log.Trace("<<<<< multipathGetPathsOfDevice")
	lines, err := MultipathdShowPaths(dev.SerialNumber)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 && dev != nil && dev.SerialNumber != "" {
		log.Errorf("no paths found for multipath device %+v", dev)
		return nil, nil
	}
	for _, line := range lines {
		log.Tracef("line is %s device serial number is %s", line, dev.SerialNumber)
		if strings.Contains(line, dev.SerialNumber) {
			entry := strings.Fields(line)
			// entry[0] uuid
			// entry[1] dev
			// entry[2] dm_st
			// entry[3] hcil
			// entry[4] dev_st
			// entry[5] chk_st
			// entry[6] checker
			// return all paths if we don't need active paths
			if !needActivePath {
				log.Tracef("needActive %v dev(%s) dm_st(%s) hcil(%s) for uuid(%s)", needActivePath, entry[1], entry[2], entry[3], entry[0])
				path := &model.PathInfo{
					UUID:     entry[0][1:],
					Device:   entry[1],
					DmState:  entry[2],
					Hcil:     entry[3],
					ChkState: entry[5],
				}
				paths = append(paths, path)
				// return only active paths with chk_st as ready and dm_st as active
			} else if len(entry) >= 5 && entry[2] == model.ActiveState.String() && entry[5] == "ready" {
				log.Tracef("needActive %v dev(%s) chk_st(%s) dm_st(%s) for uuid(%s)", needActivePath, entry[1], entry[5], entry[2], entry[0])
				path := &model.PathInfo{
					UUID:     entry[0][1:],
					Device:   entry[1],
					DmState:  entry[2],
					Hcil:     entry[3],
					ChkState: entry[5],
				}
				paths = append(paths, path)
			}
		}
	}
	if len(paths) > 0 {
		log.Debugf("paths for the device %+v are)", dev)
		for _, path := range paths {
			// set the correct state of the device if asked for active Paths
			if needActivePath && dev.State != model.ActiveState.String() && path.ChkState == "ready" {
				dev.State = model.ActiveState.String()
			}
			log.Debugf("device:%s hcil: %s state: %s", path.Device, path.Hcil, path.ChkState)
		}
	}
	return paths, nil
}

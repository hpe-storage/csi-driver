// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/mpathconfig"
	"github.com/hpe-storage/common-host-libs/sgio"
	"github.com/hpe-storage/common-host-libs/util"
	"golang.org/x/sys/unix"
)

const (
	dmsetupcommand     = "dmsetup"
	majorMinorPattern  = "(.*)\\((?P<Major>\\d+),\\s+(?P<Minor>\\d+)\\)"
	errorMapPattern    = "((?P<mapname>.*):.*error)"
	dmPrefix           = "dm-"
	dmUUIDdFormat      = "/sys/block/dm-%s/dm/uuid"
	dmNameFormat       = "/sys/block/dm-%s/dm/name"
	dmSizeFormat       = "/sys/block/dm-%s/size"
	devMapperPath      = "/dev/mapper/"
	failedDevPath      = "failed to get device path"
	notBlockDevice     = "not a block device"
	deviceDoesNotExist = "No such device or address"
	noFileOrDirErr     = "No such file or directory"
	offlinePathString  = "/sys/block/%s/device/state"
	deletePathString   = "/sys/block/%s/device/delete"
	sysBlockHolders    = "/sys/block/%s/holders/"
	holderPattern      = "^.*dm-"
	countdownTicker    = 5
	sectorstoMiBFactor = 2 * 1024
	procScsiPath       = "/proc/scsi/scsi"
	procScsiPathLocal  = "/proc_local/scsi/scsi"
	// HCTL format in /proc/scsi/scsi
	// eg Host: scsi7 Channel: 00 Id: 00 Lun: 00
	procScsiHctlPattern = "Host:\\s+scsi(?P<h>\\d+)\\s+Channel:\\s+(?P<c>\\d+)\\s+Id:\\s+(?P<t>\\d+)\\s+Lun:\\s+(?P<l>\\d+)"
	// lrwxrwxrwx 1 root root 0 Mar  8 16:51 sdg -> ../devices/platform/host4/session2/target4:0:0/4:0:0:2/block/sdg
	deviceByHctlPatternFmt = ".*%s:%s:%s:%s.*block/(?P<diskname>.*)"
	lunNotSupportedErr     = "LOGICAL UNIT NOT SUPPORTED"
)

var (
	deletingDevices   DeletingDevices
	procScsiHctlRegex = regexp.MustCompile(procScsiHctlPattern)
	errorMapRegex     = regexp.MustCompile(errorMapPattern)
)

// DeletingDevices represents serial numbers of devices being deleted
type DeletingDevices struct {
	Devices []string `json:"devices,omitempty"`
	mutex   sync.Mutex
}

func (deletingDevices *DeletingDevices) addDevice(serial string) (err error) {
	deletingDevices.mutex.Lock()
	defer deletingDevices.mutex.Unlock()

	for _, s := range deletingDevices.Devices {
		if s == serial {
			// serial is already in the list, don't add duplicates
			err = fmt.Errorf("device deletion for serial %s is already in progress", serial)
			log.Error(err.Error())
			return err
		}
	}
	deletingDevices.Devices = append(deletingDevices.Devices, serial)
	return nil
}

func (deletingDevices *DeletingDevices) removeDevice(serial string) {
	deletingDevices.mutex.Lock()
	defer deletingDevices.mutex.Unlock()

	for i, s := range deletingDevices.Devices {
		if s == serial {
			deletingDevices.Devices = append(deletingDevices.Devices[:i], deletingDevices.Devices[i+1:]...)
			return
		}
	}
}

// GetDeletingDevices returns device serial numbers in deletion state
func GetDeletingDevices() (dd *DeletingDevices) {
	deletingDevices.mutex.Lock()
	defer deletingDevices.mutex.Unlock()

	return &deletingDevices
}

// GetUserFriendlyNameBySerial returns user_friendly_name for given serial
func GetUserFriendlyNameBySerial(serial string) (mapname string, err error) {
	exists, _, _ := util.FileExists(MultipathBindings)
	if !exists {
		return "", fmt.Errorf("unable to get friendly_name for %s as bindings file %s is missing", serial, MultipathBindings)
	}
	lines, err := util.FileGetStringsWithPattern(MultipathBindings, "(?P<mapname>.*)\\s+.*"+serial)
	if err != nil {
		return "", fmt.Errorf("unable to find friendly_name from bindings file for %s err %s", serial, err.Error())
	}
	if len(lines) != 0 {
		// user_friendly_names enabled, get mpath name
		// get the last/latest map name assigned from the bindings file
		mapname = strings.Split(lines[len(lines)-1], " ")[0]
		log.Tracef("obtained map name as %s for serial %s from bindings file", mapname, serial)
		return mapname, nil
	}
	return "", fmt.Errorf("unable to find friendly_anme from bindings file for %s", serial)
}

// GetDmDeviceFromSerial returns populated multipath device object for given serial
func GetDmDeviceFromSerial(serial string) (*model.Device, error) {
	log.Tracef(">>> GetDmDeviceFromSerial called for serial %s", serial)
	defer log.Trace("<<< GetDmDeviceFromSerial")
	// assume mapname as serial by default (i.e user_friendly_names disabled)
	mapname := serial

	// If user_friendly_names is enabled, get it from multipath bindings file
	if enabled, _ := mpathconfig.IsUserFriendlyNamesEnabled(); enabled == true {
		friendlyName, err := GetUserFriendlyNameBySerial(serial)
		if err != nil {
			log.Tracef("unable to get user_friendly_names name, try to find device by serial %s, err %s", serial, err.Error())
		} else {
			mapname = friendlyName
		}
	}

	log.Tracef("getting multipath devices using dmsetup for map %s", mapname)
	args := []string{"ls", "--target", "multipath"}
	out, _, err := util.ExecCommandOutput(dmsetupcommand, args)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve multipath device with serial %s : %s", serial, err.Error())
	}

	// parse the lines into output
	// eg: mpatha	(252, 6)
	r, err := regexp.Compile(mapname + "\\s+.*(?P<major>\\d+),\\s*(?P<minor>\\d+).*")
	listOut := r.FindAllString(out, -1)
	if len(listOut) == 0 {
		// no multipath device present on host with given serial
		return nil, nil
	}
	if len(listOut) > 1 {
		return nil, fmt.Errorf("duplicate mpath exists for %s", serial)
	}

	device := &model.Device{SerialNumber: serial}
	result := util.FindStringSubmatchMap(listOut[0], r)

	// if user_friendly_names is disabled, then get the actual mpath serial(with scsi-id prefix)
	if strings.Contains(mapname, serial) {
		fileName := fmt.Sprintf(dmUUIDdFormat, result["minor"])
		mpathSerialNumber, err := util.FileReadFirstLine(fileName)
		if err != nil {
			log.Warnf("unable to retrieve device info from %s, err %s", fileName, err.Error())
			return nil, err
		}
		mapname = strings.TrimPrefix(mpathSerialNumber, "mpath-")
	}

	// path name in the format of /dev/dm-<minor>
	path := "/dev/" + dmPrefix + string(result["minor"])
	device.Pathname = path
	device.MpathName = mapname
	device.AltFullPathName = "/dev/mapper/" + mapname
	device.Minor = result["minor"]
	device.Major = result["major"]
	return device, nil
}

// GetLinuxDmDevices : Gets the list of Linux Multipath Devices
// nolint : gocyclo
func GetLinuxDmDevices(needActivePath bool, vol *model.Volume) (a []*model.Device, err error) {
	log.Tracef(">>>>>> GetLinuxDmDevices called with %s and lunID %s", vol.SerialNumber, vol.LunID)
	defer log.Trace("<<<<<< GetLinuxDmDevices")
	args := []string{"ls", "--target", "multipath"}
	out, _, err := util.ExecCommandOutput(dmsetupcommand, args)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve multipath devices")
	}

	var devices []*model.Device
	// parse the lines into output
	r, err := regexp.Compile(majorMinorPattern)
	if err != nil {
		return nil, fmt.Errorf("unable to compile regex with %s", majorMinorPattern)
	}

	listOut := r.FindAllString(out, -1)

	for _, line := range listOut {
		result := util.FindStringSubmatchMap(line, r)
		log.Tracef("Matched Map:%s", result)

		path := dmPrefix
		path += string(result["Minor"])

		device := &model.Device{
			Pathname: path,
			Major:    result["Major"],
			Minor:    result["Minor"],
		}

		err = setAltFullPathName(device)
		if err != nil {
			// if we don't get the mpath name, don't error out the workflow but continue with other devices
			log.Warnf("unable to get path for device: %s. Error: %s, continue with other devices", device.Pathname, err.Error())
			continue
		}

		fileName := fmt.Sprintf(dmUUIDdFormat, result["Minor"])
		mpathSerialNumber, err := util.FileReadFirstLine(fileName)
		if err != nil {
			// if we don't get the serial number, don't error out the workflow but continue with other devices
			log.Warnf("unable to retrieve device info from %s, continue with other devices", fileName)
			continue
		}
		// trim mpath- prefix from uuid read from /sys/block/dm-x/dm/uuid
		mpathSerialNumber = strings.TrimPrefix(mpathSerialNumber, "mpath-")
		// truncate scsi-id prefix added by multipathd(2 for EUI and 3 for NAA ID types)
		mpathSerialNumber = mpathSerialNumber[1:]
		if vol.SerialNumber != "" {
			if !strings.EqualFold(mpathSerialNumber, vol.SerialNumber) {
				log.Tracef("serialNumber %s not match with mpathSerialNumber, %s continuing..", mpathSerialNumber, vol.SerialNumber)
				continue
			}
		}

		if vol.SerialNumber == "" || strings.EqualFold(mpathSerialNumber, vol.SerialNumber) {
			device.SerialNumber = mpathSerialNumber

			sizeInMiB, err := getSizeOfDeviceInMiB(result["Minor"], device)
			if err != nil {
				return nil, err
			}
			device.Size = sizeInMiB
			var multipathdShowPaths []*model.PathInfo
			if vol.SecondaryArrayDetails != "" {
				multipathdShowPaths, err = retryGetPathOfDevice(device, false)
			} else {
				multipathdShowPaths, err = retryGetPathOfDevice(device, needActivePath)
			}

			if err != nil {
				err = fmt.Errorf("unable to get scsi slaves for device:%s. Error: %s", device.SerialNumber, err.Error())
				log.Debugf(err.Error())
				return nil, err
			}
			var slaves []string
			var hcils []string
			device.State = model.FailedState.String()
			lunIDSet := make(map[string]bool) // new empty set to keep track of unique lunIDs
			for _, path := range multipathdShowPaths {
				// check if there are some wrong LUN Ids under this multipath device,
				// return an error right here before we present this volume to be mounted
				if path.Hcil != "" {
					hcils := strings.Split(path.Hcil, ":")
					// make sure we get h:c:i:l always with the same format of length 4
					if hcils != nil && len(hcils) == 4 {
						// add all unique lun
						lunIDSet[hcils[3]] = true
						// handle lunID conflict when lunID is passed
						if vol.LunID != "" && hcils[3] != vol.LunID &&
							// lun id passed is not part of peer lun ids in case of peer persistence
							!isLunIdPresentIn(hcils[3], util.GetSecondaryArrayLUNIds(vol.SecondaryArrayDetails)) {
							device.State = model.LunIDConflict.String()
							// delete the path which have a mismatch and continue with other paths
							log.Warnf("device with serial %s has path with lunId %s instead of %s. Deleting path %s.", vol.SerialNumber, strings.Split(path.Hcil, ":")[3], vol.LunID, path.Hcil)
							deletePathByHctl(hcils[0], hcils[1], hcils[2], hcils[3])
						}
					}
				}
				// if needActive is not preent or if it's present it needs to be in ready state
				if path != nil && (!needActivePath || path.ChkState == "ready") {
					slaves = append(slaves, path.Device)
					hcils = append(hcils, path.Hcil)
				}
				if device.State != model.LunIDConflict.String() && device.State != model.ActiveState.String() && path.ChkState == "ready" {
					device.State = model.ActiveState.String() // set the state to be active if not already set if not hit with LUN ID Conflict
				}
			}

			if device.State == model.LunIDConflict.String() {
				err := fmt.Errorf("device with serial %s has path other than lunID %s. Failing request", vol.SerialNumber, vol.LunID)
				return nil, err
			}
			// if there are more than 1 lunID for the device, change the state to lunID conflict if not detected above when lunID is not passed
			if len(lunIDSet) > 1 {
				log.Warnf("device %s has paths with more than one lunID %+v, setting state to %s", device.SerialNumber, lunIDSet, model.LunIDConflict.String())
				device.State = model.LunIDConflict.String()
			}

			device.Slaves = slaves
			device.Hcils = hcils

			isFC := isFibreChannelDevice(device.Slaves)
			if !isFC {
				iscsiTargets, err := iscsiGetTargetsOfDevice(device)
				if err != nil {
					log.Debugf("unable to get iscsi target for device %s. Error: %s", device.Pathname, err.Error())
					continue
					// continue with other devices, might be in offline state and IscsiTarget will be nil for this device
				}
				if iscsiTargets != nil {
					device.IscsiTargets = iscsiTargets
				}

			}
			if len(device.Slaves) > 0 {
				log.Tracef("adding device to list %+v", device)
				devices = append(devices, device)
			}
			if vol.SerialNumber != "" {
				// break here as we found device with specific serial number
				break
			}
		}
	}
	log.Debug("Found ", len(devices), " devices")
	if len(devices) > 0 {
		for _, dev := range devices {
			log.Debugf("%+v", dev)
		}
	}
	return devices, nil
}
func isLunIdPresentIn(searchLun string, luns []int32) bool {
	for _, lun_id := range luns {
		if searchLun == strconv.Itoa(int(lun_id)) {
			return true
		}
	}
	return false
}

func getSizeOfDeviceInMiB(minorDev string, device *model.Device) (int64, error) {
	sizeFileName := fmt.Sprintf(dmSizeFormat, minorDev)
	size, err := util.FileReadFirstLine(sizeFileName)
	if err != nil {
		err = fmt.Errorf("unable to get size for device: %s Err: %s", device.Pathname, err.Error())
		return -1, err
	}
	sizeInSector, err := strconv.ParseInt(size, 10, 0)
	if err != nil {
		err = fmt.Errorf("unable to parse size for device: %s Err: %s", device.Pathname, err.Error())
		return -1, err
	}
	return sizeInSector / sectorstoMiBFactor, nil
}

//GetMpathName for device
func getMpathName(dev *model.Device) (m string, err error) {
	log.Trace(">>>>> GetMpathName")
	defer log.Trace("<<<<< GetMpathName")
	if dev.MpathName == "" {
		fileName := fmt.Sprintf(dmNameFormat, dev.Minor)
		mpathName, err := util.FileReadFirstLine(fileName)
		if err != nil {
			log.Errorf("unable to get Mpath Name from File Error:%s", err.Error())
			return "", err
		}
		dev.MpathName = mpathName
	}
	return dev.MpathName, nil
}

// setAltFullPathName for dev
func setAltFullPathName(dev *model.Device) (err error) {
	log.Trace("setAltFullPathName")
	var buffer bytes.Buffer
	mpathx, err := getMpathName(dev)
	if err != nil {
		return err
	}
	buffer.WriteString(devMapperPath)
	buffer.WriteString(mpathx)
	dev.AltFullPathName = buffer.String()
	log.Tracef("AltFullPathName:%s", dev.AltFullPathName)
	return err
}

// Helper function to perform rescan and detect the newly attached volume (FC/iSCSI volume)
func rescanLoginVolume(volume *model.Volume) error {
	log.Traceln(">>>>> rescanLoginVolume", volume, "and accessProtocol", volume.AccessProtocol)
	defer log.Traceln("<<<< rescanLoginVolume")
	var err error
	var primaryVolObj *model.Volume
	primaryVolObj = &model.Volume{}
	primaryVolObj.Name = volume.Name
	primaryVolObj.AccessProtocol = volume.AccessProtocol
	primaryVolObj.LunID = volume.LunID
	primaryVolObj.Iqns = volume.Iqns
	primaryVolObj.TargetScope = volume.TargetScope
	primaryVolObj.DiscoveryIPs = volume.DiscoveryIPs
	primaryVolObj.Chap = volume.Chap
	primaryVolObj.ConnectionMode = volume.ConnectionMode
	primaryVolObj.SerialNumber = volume.SerialNumber
	primaryVolObj.Networks = volume.Networks

	err = rescanLoginVolumeForBackend(primaryVolObj)

	if err != nil {
		return err
	}

	secondaryBackends := util.GetSecondaryBackends(volume.SecondaryArrayDetails)

	for _, secondaryLunInfo := range secondaryBackends {
		// Do iscsi discovery for Each Secondary Backend
		var secondaryVolObj *model.Volume
		secondaryVolObj = &model.Volume{}
		secondaryVolObj.Name = volume.Name
		secondaryVolObj.AccessProtocol = volume.AccessProtocol
		secondaryVolObj.LunID = strconv.Itoa(int(secondaryLunInfo.LunID))
		secondaryVolObj.Iqns = secondaryLunInfo.TargetNames
		secondaryVolObj.TargetScope = volume.TargetScope
		secondaryVolObj.DiscoveryIPs = secondaryLunInfo.DiscoveryIPs
		secondaryVolObj.Chap = volume.Chap
		secondaryVolObj.ConnectionMode = volume.ConnectionMode
		secondaryVolObj.SerialNumber = volume.SerialNumber
		secondaryVolObj.Networks = volume.Networks

		err = rescanLoginVolumeForBackend(secondaryVolObj)
		if err != nil {
			return err
		}
	}
	return nil
}

func rescanLoginVolumeForBackend(volObj *model.Volume) error {

	log.Traceln("Called rescanLoginVolumeForBackend", volObj, "and accessProtocol", volObj.AccessProtocol)
	var err error
	if strings.EqualFold(volObj.AccessProtocol, "fc") {
		// FC volume

		err = RescanFcTarget(volObj.LunID)
		if err != nil {
			return err
		}
	} else {
		// Check if client intends us to specifically login using multiple IP addresses(cloud volumes)
		if len(volObj.Networks) > 0 {
			// check if ifaces are created and enable port binding
			err = addIscsiPortBinding(volObj.Networks)
			if err != nil {
				return err
			}
		}
		// iSCSI volume
		err = HandleIscsiDiscovery(volObj)
		if err != nil {
			return err
		}
	}
	return nil
}

func isLuksDevice(devPath string) (bool, error) {
	log.Tracef(">>>> isLuksDevice - %s", devPath)
	defer log.Tracef("<<<< isLuksDevice - %s", devPath)

	_, exitStatus, _ := util.ExecCommandOutput("cryptsetup", []string{"isLuks", "-v", devPath})
	log.Tracef("Exit status of isLuks: %d", exitStatus)
	if exitStatus == 0 {
		log.Infof("%s is a LUKS device", devPath)
		return true, nil
	}
	if exitStatus == 1 {
		log.Infof("Not a LUKS device - %s", devPath)
		return false, nil
	} else {
		log.Errorf("Unknown device - %s", devPath)
		return false, fmt.Errorf("isLuks command failed to find out if the device %s is encrypted", devPath)
	}
}

// CreateLinuxDevice : attaches and creates a new linux device
// nolint: gocyclo
func createLinuxDevice(volume *model.Volume) (dev *model.Device, err error) {
	log.Debugf(">>>> createLinuxDevice called with volume %s serialNumber %s and lunID %s", volume.Name, volume.SerialNumber, volume.LunID)
	defer log.Debug("<<<<< createLinuxDevice")
	// Rescan and detect the newly attached volume
	err = rescanLoginVolume(volume)
	if err != nil {
		return nil, err
	}
	log.Tracef("sleeping for 1 second waiting for device %s to appear after rescan", volume.SerialNumber)
	time.Sleep(time.Second * 1)
	// find multipath devices after the rescan and login
	// Start a Countdown ticker
	var devices []*model.Device
	for i := 0; i <= countdownTicker; i++ {
		devices, err = GetLinuxDmDevices(true, volume)
		if err != nil {
			return nil, err
		}
		for _, d := range devices {
			// ignore devices with no paths
			if len(d.Slaves) == 0 {
				continue
			}
			// Match SerialNumber
			if d.SerialNumber == volume.SerialNumber {
				log.Debugf("Found device with matching SerialNumber:%s map %s and slaves %+v", d.SerialNumber, d.AltFullPathName, d.Slaves)

				if volume.EncryptionKey != "" {
					originalDevPath := "/dev/" + d.Pathname

					isLuksDev, err := isLuksDevice(originalDevPath)
					if err != nil {
						return nil, err
					}

					// LUKS format device if this is the first time it is being used
					if !isLuksDev {
						log.Infof("Device %s is a new device. LUKS formatting it...", originalDevPath)
						_, _, err := util.ExecCommandOutputWithStdinArgs("cryptsetup",
							[]string{"luksFormat", "--type", "luks1", "--batch-mode", originalDevPath},
							[]string{volume.EncryptionKey})
						if err != nil {
							err = fmt.Errorf("LUKS format command failed with error: %v", err)
							log.Error(err.Error())
							return nil, err
						}
						log.Infof("Device %s has been LUKS formatted successfully", originalDevPath)
					}
					srcMpath := "/dev/" + d.Pathname
					mappedMPath := "enc-" + d.MpathName // "enc-mpathx"
					log.Infof("Opening LUKS device %s with mapped device %s...", srcMpath, mappedMPath)
					_, _, err = util.ExecCommandOutputWithStdinArgs("cryptsetup",
						[]string{"luksOpen", srcMpath, mappedMPath},
						[]string{volume.EncryptionKey})
					if err != nil {
						err = fmt.Errorf("LUKS open command failed with error: %v", err)
						log.Error(err.Error())
						return nil, err
					}
					log.Infof("Opened LUKS device %s with mapped device %s successfully", srcMpath, mappedMPath)

					// Replacing the device path,AltFullPathName
					d.LuksPathname = mappedMPath
					d.AltFullLuksPathName = "/dev/mapper/" + mappedMPath
				}
				log.Debugf("Returning device with matching serialNumber:%s map %s and slaves %+v", d.SerialNumber, d.AltFullPathName, d.Slaves)
				return d, nil
			}
			// Match TargetName/IQN only for VST type
			if d.IscsiTargets[0] != nil && volume.SerialNumber == "" && d.IscsiTargets[0].Name == volume.Iqn {
				log.Debugf("Found device with matching target-name:%s scope:%s map:%s and slaves %+v", d.IscsiTargets[0].Name, d.IscsiTargets[0].Scope, d.AltFullPathName, d.Slaves)

				return d, nil
			}
		}
		// check if its a remapped LUN case
		handleRemappedLun(volume)
		// check if there are orphan paths during attach
		handleOrphanPaths(volume)
		// check any error maps are present and cleanup
		cleanupErrorMultipathMaps()
		log.Debugf("sleeping for 5 seconds waiting for device %s to appear after rescan", volume.SerialNumber)
		time.Sleep(time.Second * 5)
	}
	// cleanup paths which maybe part of lsscsi but not in multipath as we haven't found the device yet
	cleanupStaleScsiPaths(volume)
	// try to logout the iscsi target if we could not find a device by now for VST Volume
	if volume.TargetScope != GroupScope.String() && len(volume.TargetNames()) != 0 {
		iscsiLogoutOfTarget(&model.IscsiTarget{Name: volume.TargetNames()[0]})
	}
	// Reached here signifies the device was not found, throw an error
	return nil, fmt.Errorf("device not found with serial %s or target %s", volume.SerialNumber, volume.Iqn)
}

//nolint : gocyclo
func cleanupStaleScsiPaths(volume *model.Volume) (err error) {
	log.Tracef(">>>>> cleanupStaleScsiPaths called for %s", volume.Name)
	defer log.Tracef("<<<<<< cleanupStaleScsiPaths")

	if volume.LunID == "" || volume.SerialNumber == "" {
		err := fmt.Errorf("LunID and SerialNumber is needed for cleanup of stale scsi devices for %s", volume.SerialNumber)
		log.Warn(err.Error())
		return err
	}
	procScsiRealPath := getProcScsiPath()
	lunID := volume.LunID
	if len(volume.LunID) == 1 {
		// format it into /proc/scsi/scsi format for matching eg: Lun: 00
		lunID = "0" + volume.LunID
	}

	// example output from /proc/scsi/scsi
	// Host: scsi7 Channel: 00 Id: 00 Lun: 00
	paths, err := util.FileGetStringsWithPattern(procScsiPath, "(.*Lun: "+lunID+")")
	if err != nil {
		// unable to obtain lsscsi output, return
		return err
	}

	var pathFound bool
	pathFound = false
	for _, pathStr := range paths {
		// parse h:c:t:l info
		h, c, t, l, err := parseHctl(pathStr)
		if err != nil {
			log.Debugf("unable to parse h:c:t:l info from %s, err %s", procScsiRealPath, err.Error())
			// continue with other paths as best effort
			continue
		}

		// obtain current path serial using sysfs vpd80 page
		var serialNumber string
		vendorName, _ := getVendorFromSysfs(h, c, t, l)
		if strings.Contains(vendorName, "3PARdata"){
			serialNumber, err = getWwidFromSysfs(h, c, t, l)
		}else {
		// obtain current path serial using sysfs vpd80 page
			serialNumber, err = getVpd80FromSysfs(h, c, t, l)
		}
		//serialNumber, err = getVpd80FromSysfs(h, c, t, l)
		if err != nil {
			log.Debugf("unable to read vpd_pg80 from sysfs for %s:%s:%s:%s err=%s. Continue with other paths", h, c, t, l, err.Error())
			continue
		}
		if serialNumber != volume.SerialNumber {
			// try to get the serialNumber from inquiry if it didn't match
			serialNumber, _ = getDeviceSerialByHctl(h, c, t, l)
		}
		log.Tracef("got serialNumber %s for %s:%s:%s:%s, volume serialNumber is %s", serialNumber, h, c, t, l, volume.SerialNumber)
		// delete the paths only for the current serial and lun for the device create to go through
		if serialNumber == volume.SerialNumber && l == volume.LunID {
			pathFound = true
			deletePathByHctl(h, c, t, l)
		}
	}
	// give it 1 second to delete the paths if found
	if pathFound {
		time.Sleep(time.Second)
	}
	return nil
}

// check if required properties are present in volume to handle remap
func canHandleRemap(volume *model.Volume) (err error) {
	// remap cannot happen for iSCSI VST volumes
	if !strings.EqualFold(volume.AccessProtocol, "fc") && volume.TargetScope == "" {
		return fmt.Errorf("cannot handle remap for VST volume %s", volume.Name)
	}

	// cannot handle remap if lun id is not provided
	if volume.LunID == "" {
		return fmt.Errorf("cannot handle remap, lun id not provided for volume %s", volume.Name)
	}

	// cannot handle remap if volume serial is not provided
	if volume.SerialNumber == "" {
		return fmt.Errorf("cannot handle remap, serial not provided for volume %s", volume.Name)
	}
	return nil
}

func handleOrphanPaths(volume *model.Volume) error {
	log.Tracef(">>> handleOrphanPaths called with volume %s serial %s lun %s", volume.Name, volume.SerialNumber, volume.LunID)
	defer log.Tracef("<<< handleOrphanPaths")
	err := handleOrphanPathsForSpecificLunId(volume.LunID)
	if err != nil {
		return err
	}
	lunIdArray := util.GetSecondaryArrayLUNIds(volume.SecondaryArrayDetails)
	if len(lunIdArray) > 0 {
		for _, lunID := range lunIdArray {
			err := handleOrphanPathsForSpecificLunId(strconv.Itoa(int(lunID)))
			if err != nil {
				return err
			}
		}
	}
	return nil

}

func handleOrphanPathsForSpecificLunId(lunID string) error {

	orphanHctls := multipathGetOrphanPathsByLunID(lunID)
	for _, pathHctl := range orphanHctls {
		hctl := strings.Split(pathHctl, ":")
		if len(hctl) != 4 {
			continue
		}
		// delete the orphan path
		deletePathByHctl(hctl[0], hctl[1], hctl[2], hctl[3])
		// rescan host adapater for the same h:c:t:l
		fcHostScanPath := fmt.Sprintf(fcHostScanPathFormat, hctl[0])
		argsScan := fmt.Sprintf("%s %s %s", hctl[1], hctl[2], hctl[3])
		isFileExist, _, _ := util.FileExists(fcHostScanPath)
		if isFileExist {
			err := ioutil.WriteFile(fcHostScanPath, []byte(argsScan), 0644)
			if err != nil {
				// ignore error as we can still find device from host scans
				log.Debugf("error writing to file %s : %s", fcHostScanPath, err.Error())
			}
		}
	}
	return nil
}

// find if an existing lun has been remapped with new lun provided
// if found, remove old paths and rescan new lun paths
// nolint : gocyclo
func handleRemappedLun(volume *model.Volume) (err error) {
	log.Tracef(">>> handleRemappedLun called with volume %s serial %s lun %s", volume.Name, volume.SerialNumber, volume.LunID)
	defer log.Tracef("<<< handleRemappedLun")

	err = canHandleRemap(volume)
	if err != nil {
		log.Trace(err.Error())
		return err
	}

	lunID := volume.LunID
	if len(volume.LunID) == 1 {
		// format it into /proc/scsi/scsi format for matching eg: Lun: 00
		lunID = "0" + volume.LunID
	}
	err = handleRemapForSpecificLunID(volume.SerialNumber, lunID, volume.AccessProtocol)
	if err != nil {
		return err
	}
	lunIdArray := util.GetSecondaryArrayLUNIds(volume.SecondaryArrayDetails)
	if len(lunIdArray) > 0 {
		for _, secLunID := range lunIdArray {
			err = handleRemapForSpecificLunID(volume.SerialNumber, "0"+strconv.Itoa(int(secLunID)), volume.AccessProtocol)
			if err != nil {
				return err
			}
		}
	}
	return nil

}
func handleRemapForSpecificLunID(serialNumber, lunID, accessProtocol string) (err error) {
	// example output from /proc/scsi/scsi
	// Host: scsi7 Channel: 00 Id: 00 Lun: 00
	procScsiRealPath := getProcScsiPath()

	paths, err := util.FileGetStringsWithPattern(procScsiRealPath, "(.*Lun: "+lunID+")")
	if err != nil {
		// unable to obtain lsscsi output, return
		return err
	}

	for _, pathStr := range paths {
		// parse h:c:t:l info
		h, c, t, l, err := parseHctl(pathStr)
		if err != nil {
			log.Debugf("unable to parse h:c:t:l info from %s, err %s", procScsiRealPath, err.Error())
			// continue with other paths as best effort
			continue
		}
		// refresh path serial and update paths if its remapped lun
		remappedLunFound, oldSerial, err := checkRemappedLunPath(h, c, t, l, serialNumber, lunID)
		if err != nil {
			log.Debugf("error occurred while checking for remapped Lun on %s, err %s", serialNumber, err.Error())
			// continue with other paths as best effort
			continue
		}
		if !remappedLunFound || oldSerial == "" {
			// continue with other paths
			continue
		}
		// cleanup multipath device and all its paths, as we found its remapped with different LUN underneath
		cleanupUnmappedDevice(oldSerial, lunID)
		// perform SCSI lun rescan to discover new volume paths
		if strings.EqualFold(accessProtocol, "fc") {
			RescanFcTarget(l)
		} else {
			RescanIscsi(l)
		}
		// we found the remapped volume, we can skip rest of the paths as they are cleaned up at once above
		break
	}
	return nil

}

func getProcScsiPath() string {
	isLocalPathExists, _, _ := util.FileExists(procScsiPathLocal)
	if isLocalPathExists {
		return procScsiPathLocal
	}
	return procScsiPath
}

// cleanup unmapped device from top to bottom
// nolint: gocyclo
func cleanupUnmappedDevice(oldSerial string, volumelunID string) error {
	log.Tracef(">>> cleanupUnmappedDevice called with serial %s lunID %s", oldSerial, volumelunID)
	defer log.Traceln("<<< cleanupUnmappedDevice")
	var devices []*model.Device
	var err error
	devices, err = GetLinuxDmDevices(false, util.GetVolumeObject(oldSerial, ""))
	if err != nil {
		log.Errorf("error occurred while getting unmapped device for serial %s, err %s", oldSerial, err.Error())
		return err
	}
	if len(devices) > 0 {
		// got old multipath device, cleanup along with paths
		// remapped devices only occur with GST type(FC, iSCSI)
		devices[0].TargetScope = GroupScope.String()
		err = DeleteDevice(devices[0])
		if err != nil {
			// check if the device is currently mounted and unmount to prevent further corruption
			// since we determined that LUN has been remapped
			if strings.Contains(err.Error(), errCurrentlyMounted) && devices[0].Hcils != nil {
				deviceHcil := strings.Split(devices[0].Hcils[0], ":")
				if len(deviceHcil) != 4 {
					return fmt.Errorf("unable to parse device hcil for %v ", devices[0])
				}
				lunIDFromMultipath := deviceHcil[3]
				// get all slaves and match the lunID of the slaves for the intended volumeLunID
				lunID := volumelunID
				if len(volumelunID) == 1 {
					// format it into /proc/scsi/scsi format for matching eg: Lun: 00
					lunID = "0" + volumelunID
				}
				var paths []string
				procScsiRealPath := getProcScsiPath()

				paths, err = util.FileGetStringsWithPattern(procScsiRealPath, "(.*Lun: "+lunID+")")
				if err != nil {
					// unable to obtain lsscsi output, return
					return err
				}
				// e.g. if lunIDFromMultipath is 0 for the serial whereas volumeLunID is 1 then we should not unmount
				if volumelunID == lunIDFromMultipath {
					mounts, _ := GetMountPointsForDevices(devices)
					for _, mount := range mounts {
						log.Infof("unmounting device at %s due to lun remap of lunID=%s", mount.Mountpoint, volumelunID)
						// try to unmount
						_ = unmount(mount.Mountpoint)
					}
				} else {
					// delete paths for lunId=volumelunID as they are not allowing to discover new paths
					for _, pathStr := range paths {
						var h, c, t, l string
						h, c, t, l, err = parseHctl(pathStr)
						if err != nil {
							log.Debugf("unable to parse path %s err=%s, continue with other paths", pathStr, err.Error())
							continue
						}
						var currentSerial string
						vendorName, _ := getVendorFromSysfs(h, c, t, l)
						if strings.Contains(vendorName, "3PARdata"){
							currentSerial, err = getWwidFromSysfs(h, c, t, l)
						}else {
						// obtain current path serial using sysfs vpd80 page
							currentSerial, err = getVpd80FromSysfs(h, c, t, l)
						}						
						
						//currentSerial, err = getVpd80FromSysfs(h, c, t, l)
						if err != nil {
							log.Debugf("unable to get serial for h:c:t:l %s:%s:%s:%s err=%s, continue with other paths", h, c, t, l, err.Error())
							continue
						}
						// check for serial number from vpd page before deleting
						if currentSerial == oldSerial {
							deletePathByHctl(h, c, t, l)
						}
					}
				}
			} else {
				log.Warnf("unable to cleanup unmapped device %s with serial %s", devices[0].MpathName, oldSerial)
				return err
			}
		}
	} else {
		// multipath might be deleted, but paths are lying around. Cleanup paths and rescan.
		var paths []*model.PathInfo
		paths, err = multipathGetPathsOfDevice(&model.Device{SerialNumber: oldSerial}, false)
		if err != nil {
			log.Warnf("unable to get unmapped paths with serial %s err=%s", oldSerial, err.Error())
		}
		for _, path := range paths {
			// get host, channel, target, lun in the format of H:C:T:L
			hctl := strings.Split(path.Hcil, ":")
			if len(hctl) != 4 {
				continue
			}
			err := deletePathByHctl(hctl[0], hctl[1], hctl[2], hctl[3])
			if err != nil {
				log.Warnf("unable to update remapped lun path %s, err %s", path.Hcil, err.Error())
				// continue with other paths
			}
		}
	}
	return nil
}

// check if a lun path has been remapped and update paths with new lun
// serial in this case is volume serial (i.e without prefix 2) as we are directly getting from vpd page 80
func checkRemappedLunPath(h string, c string, t string, l string, serial string, lunID string) (remappedLunFound bool, oldSerial string, err error) {
	log.Tracef(">>>>> checkRemappedLunPath")
	// check if the path is in offline state and assume its a stale path with same lunID so we rescan the path again.
	state, err := getDeviceState(h, c, t, l)
	if err != nil {
		return false, "", err
	}
	vendorName, _ := getVendorFromSysfs(h, c, t, l)
	if strings.Contains(vendorName, "3PARdata"){
		oldSerial, err = getWwidFromSysfs(h, c, t, l)
	}else {
		// obtain current path serial using sysfs vpd80 page
		oldSerial, err = getVpd80FromSysfs(h, c, t, l)
	}
	
	if err != nil {
		return false, "", err
	}
	if oldSerial == serial {
		// found lun with actual serial number, no need to update
		return false, "", nil
	}

	if state == "offline" {
		// assume remapped lun due to stale offline path
		log.Debugf("%s:%s:%s:%s lun-id %s is in offline state, assuming remapped-lun", h, c, t, l, lunID)
		return true, oldSerial, nil
	}
   	if strings.Contains(vendorName, "3PARdata"){
			log.Debugf("found remapped lun with oldserial %s and Serial %s", oldSerial, serial)		
	} else{
	// check for serial number mismatch
		updatedSerial, err := getDeviceSerialByHctl(h, c, t, l)
		if err != nil {
			// continue with other paths
			return false, "", err
		}

		if updatedSerial != serial {
			return false, "", nil
		}
		log.Debugf("found remapped lun with oldserial %s and updatedSerial %s", oldSerial, updatedSerial)		
	}
	
	// updated path serial matches serial of the new lun attached

	return true, oldSerial, nil
}

// nolint as we want to keep updatePathSerialByHctl separate
func deletePathByHctl(h string, c string, t string, l string) (err error) {
	log.Tracef(">>>>> deletePathByHctl called with h:c:t:l %s:%s:%s:%s", h, c, t, l)
	defer log.Trace("<<<<< deletePathByHctl")
	deletePath := fmt.Sprintf("/sys/class/scsi_device/%s:%s:%s:%s/device/delete", h, c, t, l)
	is, _, _ := util.FileExists(deletePath)
	if is {
		err := ioutil.WriteFile(deletePath, []byte("1"), 0644)
		if err != nil {
			log.Debugf("error writing to file %s : %s", deletePath, err.Error())
			return err
		}
	}
	return nil
}

func getDeviceSerialByHctl(h string, c string, t string, l string) (serial string, err error) {
	// get the scsi device based on hctl
	// lrwxrwxrwx 1 root root 0 Mar  8 16:51 sdg -> ../devices/platform/host4/session2/target4:0:0/4:0:0:2/block/sdg
	disk := "/dev/"
	args := []string{"-ltr", "/sys/block/"}
	out, _, err := util.ExecCommandOutput("ls", args)
	if err != nil {
		return "", fmt.Errorf("unable to find scsi disk device by hctl %s:%s:%s:%s err %s", h, c, t, l, err.Error())
	}

	hctlPattern := fmt.Sprintf(deviceByHctlPatternFmt, h, c, t, l)
	deviceByHctlRegex, err := regexp.Compile(hctlPattern)
	if err != nil {
		return "", fmt.Errorf("unable to create regex to get serial for hctl %s:%s:%s:%s err %s", h, c, t, l, err.Error())
	}
	if !deviceByHctlRegex.MatchString(out) {
		return "", fmt.Errorf("unable to match scsi disk device by hctl %s:%s:%s:%s", h, c, t, l)
	}

	diskMap := util.FindStringSubmatchMap(out, deviceByHctlRegex)
	if _, ok := diskMap["diskname"]; !ok {
		return "", fmt.Errorf("unable to match scsi disk device by hctl %s:%s:%s:%s", h, c, t, l)
	}

	disk += diskMap["diskname"]
	serial, err = sgio.GetDeviceSerial(disk)
	if err != nil {
		return "", fmt.Errorf("unable to get device serial for %s err %s", disk, err.Error())
	}
	log.Tracef("obtained serial as %s for device %s", serial, disk)
	return serial, nil
}

func getDeviceState(h string, c string, t string, l string) (state string, err error) {
	statePath := fmt.Sprintf("/sys/class/scsi_device/%s:%s:%s:%s/device/state", h, c, t, l)
	state, err = util.FileReadFirstLine(statePath)
	if err != nil {
		return "", err
	}
	return state, nil
}

func getVendorFromSysfs(h string, c string, t string, l string) (vendorName string, err error){
	log.Tracef(">>>>> getVendorFromSysfs")
	vendorPath := fmt.Sprintf("/sys/class/scsi_device/%s:%s:%s:%s/device/vendor", h, c, t, l)
	out, err := util.FileReadFirstLine(vendorPath)
	log.Info("vendor: ", out)
	if err != nil {
		return "", err
	}
    return out, nil
}

func getWwidFromSysfs(h string, c string, t string, l string) (serial string, err error){

	log.Tracef(">>>>> getWwidFromSysfs")

	wwidPath := fmt.Sprintf("/sys/class/scsi_device/%s:%s:%s:%s/device/wwid", h, c, t, l)
	wwidOut, wwidErr := util.FileReadFirstLine(wwidPath)
	if wwidErr != nil {
		return "", wwidErr
	}
	log.Info("wwidOut", wwidOut)
	// extract serial from format: naa.60002ac0000000000a0066ef0001db2c
	entries := strings.Split(wwidOut, ".")
	if len(entries) > 1 {
	   return entries[1], nil
	}
	return "", fmt.Errorf("invalid serial number found with wwid %s", wwidOut)
	
	
}

func getVpd80FromSysfs(h string, c string, t string, l string) (serial string, err error) {
	vpdPath := fmt.Sprintf("/sys/class/scsi_device/%s:%s:%s:%s/device/vpd_pg80", h, c, t, l)
	out, err := util.FileReadFirstLine(vpdPath)
	if err != nil {
		return "", err
	}
	// extract serial from format: ^@<80>^@ f33577bbf4b1e2036c9ce90029989377
	entries := strings.Split(out, " ")
	if len(entries) > 1 {
		return entries[1], nil
	}
	return "", fmt.Errorf("invalid serial number found with vpd_pg80 %s", out)
}

// parse host channel target lun identifiers with entry from /proc/scsi/scsi
// eg string Host: scsi7 Channel: 00 Id: 00 Lun: 00
func parseHctl(lunStr string) (h string, c string, t string, l string, err error) {
	if !procScsiHctlRegex.MatchString(lunStr) {
		return "", "", "", "", fmt.Errorf("unable to find h:c:t:l pattern from provided lun string %s", lunStr)
	}

	hctlMap := util.FindStringSubmatchMap(lunStr, procScsiHctlRegex)
	if val, ok := hctlMap["h"]; ok {
		h = val
	}
	if val, ok := hctlMap["c"]; ok {
		c = strings.TrimPrefix(val, "0")
	}
	if val, ok := hctlMap["t"]; ok {
		t = strings.TrimPrefix(val, "0")
	}
	if val, ok := hctlMap["l"]; ok {
		l = strings.TrimPrefix(val, "0")
	}

	return h, c, t, l, nil
}

// GetDeviceFromVolume returns Linux device for given volume info
func GetDeviceFromVolume(vol *model.Volume) (*model.Device, error) {
	log.Tracef(">>>>> GetDeviceFromVolume for serial %s", vol.SerialNumber)
	defer log.Trace("<<<<< GetDeviceFromVolume")

	devices, err := GetLinuxDmDevices(false, vol)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("unable to find device matching volume serial number %s", vol.SerialNumber)
	}
	return devices[0], nil
}

//CreateLinuxDevices : attached and creates linux devices to host
func CreateLinuxDevices(vols []*model.Volume) (devs []*model.Device, err error) {
	log.Tracef(">>>>> CreateLinuxDevices")
	defer log.Trace("<<<<< CreateLinuxDevices")

	var devices []*model.Device
	for _, vol := range vols {
		log.Tracef("create request with serialnumber :%s, accessprotocol %s discoveryip %s, iqn %s ", vol.SerialNumber, vol.AccessProtocol, vol.DiscoveryIP, vol.Iqn)
		if vol.AccessProtocol == iscsi && (vol.DiscoveryIP == "" || vol.Iqn == "") && len(vol.DiscoveryIPs) == 0 {
			return nil, fmt.Errorf("cannot discover without IP. Please sanity check host OS and array IP configuration, network, netmask and gateway")
		}
		device, err := createLinuxDevice(vol)
		if err != nil {
			log.Errorf("unable to create device for volume %v with IQN %v", vol.Name, vol.Iqn)
			// If we encounter an error, there may be some devices created and some not.
			// If we fail the operation , then we need to rollback all the created device
			//TODO : cleanup all the devices created
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, nil
}

// OfflineDevice : offline all the scsi paths for the multipath device
func OfflineDevice(dev *model.Device) (err error) {
	log.Tracef("OfflineDevice called with %s", dev.SerialNumber)
	// perform offline scsi paths of the multipath device
	if dev.SerialNumber == "" {
		return fmt.Errorf("no serialNumber of device %v present, failing delete", dev)
	}
	paths, err := retryGetPathOfDevice(dev, false)
	if len(paths) == 0 {
		// if no paths exists to offline, don't treat this as an error but proceed ahead
		return nil
	}
	if err != nil {
		return fmt.Errorf("error to offline the scsi paths %v : %s", paths, err.Error())
	}
	//offline the paths
	return offlineScsiDevices(paths)
}

func offlineScsiDevices(paths []*model.PathInfo) error {
	log.Tracef("offlineScsiDevices called with %v", paths)
	for _, path := range paths {
		err := offlineScsiDevice(path.Device)
		if err != nil {
			return fmt.Errorf("failed to offline scsi device %s. Error: %s", path.Device, err.Error())
		}
	}
	return nil
}

func offlineScsiDevice(path string) (err error) {
	log.Tracef("offlineScsiDevice called with %s", path)
	//offline the path
	offlinePath := fmt.Sprintf(offlinePathString, path)
	is, _, _ := util.FileExists(offlinePath)
	if !is {
		err = fmt.Errorf("path %s doesn't exist", offlinePath)
		log.Warn(err.Error())
		return err
	}
	if is {
		err = ioutil.WriteFile(offlinePath, []byte("offline\n"), 0644)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteDevice : delete the multipath device
func DeleteDevice(dev *model.Device) (err error) {
	log.Tracef("DeleteDevice called with %s", dev.SerialNumber)
	// perform cleanup of the multipath device
	if dev.SerialNumber == "" {
		return fmt.Errorf("no serialNumber of device %v present, failing delete", dev)
	}

	//check if device is mounted or has holders
	err = checkIfDeviceCanBeDeleted(dev)
	if err != nil {
		return err
	}

	err = deletingDevices.addDevice(dev.SerialNumber)
	if err != nil {
		// indicates device deletion is already in progress, don't error out
		return nil
	}
	defer deletingDevices.removeDevice(dev.SerialNumber)
	err = retryTearDownMultipathDevice(dev)
	if err != nil {
		return err
	}
	return nil
}

// flush the device buffers
func flushbufs(dev *model.Device) error {
	log.Tracef("flushbufs called for %+v", dev)
	if dev == nil && dev.AltFullPathName == "" {
		return fmt.Errorf("device.AltFullPathName %+v not present to perform flushbufs", dev)
	}
	args := []string{"--flushbufs", dev.AltFullPathName}
	out, _, _ := util.ExecCommandOutputWithTimeout("blockdev", args, 15)
	if out != "" {
		log.Tracef("output from flushbufs on %+v: %s", dev, out)
	}
	return nil
}

// retry the tearDown if there is failure
func retryTearDownMultipathDevice(dev *model.Device) error {
	try := 0
	for {
		err := tearDownMultipathDevice(dev)
		if err != nil {
			if try < maxTries {
				try++
				time.Sleep(time.Duration(try) * time.Second)
				continue
			}
			return err
		}
		return nil
	}
}

// deleteSdDevice : delete the scsi device
func deleteSdDevice(path string) (err error) {
	log.Tracef("deleteSdDevice called with %s", path)
	//deletePath for deleting the device
	deletePath := fmt.Sprintf(deletePathString, path)
	is, _, _ := util.FileExists(deletePath)
	if !is {
		// path seems to be already cleaned up so we return success
		err = fmt.Errorf("path %s doesn't exist", deletePath)
		log.Debug(err.Error())
		return nil
	}
	err = ioutil.WriteFile(deletePath, []byte("1\n"), 0644)
	if err != nil {
		log.Debugf("error writing to file %s : %s", deletePath, err.Error())
		return err
	}
	return nil
}

// getDeviceHolders : get the holders of the scsi device if any
func getDeviceHolders(dev *model.Device) (h string, err error) {
	var holder string
	log.Tracef("getDeviceHolders called")
	var re = regexp.MustCompile(holderPattern)
	log.Tracef("Path =  %s", dev.Pathname)
	directoryPath := fmt.Sprintf(sysBlockHolders, dev.Pathname)
	// holders folder might not exist in some flavors
	dirExists, _, err := util.FileExists(directoryPath)
	if !dirExists {
		return "", nil
	}
	// walk all files in directory
	err = filepath.Walk(directoryPath, func(path string, info os.FileInfo, err error) error {
		if info != nil && !info.IsDir() {
			//check for valid symlink
			var link string
			link, err = os.Readlink(filepath.Join(directoryPath, info.Name()))
			if err != nil {
				log.Debugf("error reading the symlink" + directoryPath + info.Name())
			} else {
				holder = re.ReplaceAllString(link, "dm-")
				log.Tracef("Found holder: %s for device: %s ", holder, dev.Pathname)
			}
		}
		return err
	})

	if err != nil {
		return "", err
	}

	return holder, nil
}

// RescanSize performs size rescan of all scsi devices on host and updates applicable multipath devices
// TODO: replace rescan-scsi-bus.sh dependency with manual rescan of scsi devices
func RescanForCapacityUpdates(devicePath string) error {
	log.Tracef(">>>>> RescanForCapacityUpdates called for %s", devicePath)
	defer log.Traceln("<<<<< RescanForCapacityUpdates")

	args := []string{"-s", "-m"}
	_, _, err := util.ExecCommandOutput("rescan-scsi-bus.sh", args)
	if err != nil {
		return err
	}

	// The underlying device found can be a mapped LUKS device (enc-mpathx) in which case it needs
	// to be resized using LUKS command
	isMappedLuksDev, err := isMappedLuksDevice(devicePath)
	if err != nil {
		return err
	}

	if isMappedLuksDev {
		_, err := resizeMappedLuksDevice(devicePath)
		return err
	}

	if devicePath != "" {
		// multipathd takes either mpathx(without prefix) or /dev/dm-x as input
		devicePath = strings.TrimPrefix(devicePath, "/dev/mapper/")
		// reload multipath map to apply new size
		args = []string{"resize", "map", devicePath}
		out, _, err := util.ExecCommandOutput("multipathd", args)
		if err != nil {
			return err
		}
		if !strings.Contains(out, "ok") {
			return fmt.Errorf("failed to rescan device %s on resize, err: %s", devicePath, out)
		}
	}
	return nil
}

// IsBlockDevice checks if the given path is a block device
func IsBlockDevice(devicePath string) (bool, error) {
	var st unix.Stat_t
	err := unix.Stat(devicePath, &st)
	if err != nil {
		return false, err
	}

	return (st.Mode & unix.S_IFMT) == unix.S_IFBLK, nil
}

// GetBlockSizeBytes returns the block size in bytes
func GetBlockSizeBytes(devicePath string) (int64, error) {
	args := []string{"--getsize64", devicePath}
	out, _, err := util.ExecCommandOutput("blockdev", args)
	if err != nil {
		return -1, fmt.Errorf("error when getting size of block volume at path %s: output: %s, err: %v", devicePath, string(out), err)
	}
	strOut := strings.TrimSpace(string(out))
	gotSizeBytes, err := strconv.ParseInt(strOut, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("failed to parse size %s as int", strOut)
	}
	return gotSizeBytes, nil
}

// ExpandDevice expands device and filesystem at given targetPath to underlying volume size
// targetPath is /dev/dm-* for block device and mountpoint for filesystem based device
func ExpandDevice(targetPath string, volAccessType model.VolumeAccessType) error {
	log.Tracef(">>>>> ExpandDevice called with targetPath: %s, volAccessType: %s", targetPath, volAccessType.String())
	defer log.Traceln("<<<<< ExpandDevice")

	// If Block volume access type
	if volAccessType == model.BlockType {
		// resize device with targetPath, i.e /dev/dm-*
		if err := RescanForCapacityUpdates(targetPath); err != nil {
			return fmt.Errorf("unable to perform rescan to update device capacity for %s, error :%s",
				targetPath, err.Error())
		}
		return nil
	}

	// Else Mount volume access type
	// check if mount point exists and get underlying device
	devPath, err := GetDeviceFromMountPoint(targetPath)
	if err != nil || devPath == "" {
		return fmt.Errorf("unable to get device mounted at %s", targetPath)
	}

	// resize device with devPath, i.e /dev/mapper/mpath*
	err = RescanForCapacityUpdates(devPath)
	if err != nil {
		return fmt.Errorf("unable to perform rescan to update device capacity for %s, error :%s", devPath, err.Error())
	}
	// resize filesystem
	fsType, err := GetFilesystemType(devPath)
	if err != nil {
		return fmt.Errorf("unable to determine filesystem type on %s, error :%s", devPath, err.Error())
	}
	err = ExpandFilesystem(devPath, targetPath, fsType)
	if err != nil {
		return fmt.Errorf("unable to expand filesystem on %s, error :%s", devPath, err.Error())
	}
	return err
}

func resizeMappedLuksDevice(devPath string) (bool, error) {
	log.Tracef(">>>> resizeMappedLuksDevice - %s", devPath)
	defer log.Tracef("<<<< resizeMappedLuksDevice - %s", devPath)

	_, exitStatus, err := util.ExecCommandOutput("cryptsetup", []string{"resize", devPath})
	log.Tracef("exit status of resize LUKS device: %d", exitStatus)
	if exitStatus == 0 {
		log.Infof("mapped LUKS device %s resized successfully", devPath)
		return true, nil
	}
	errMsg := fmt.Errorf("failed to resize mapped LUKS device %s with error: %v", devPath, err)
	log.Errorln(errMsg.Error())
	return false, errMsg
}

func isMappedLuksDevice(devPath string) (bool, error) {
	log.Tracef(">>>> isMappedLuksDevice - %s", devPath)
	defer log.Tracef("<<<< isMappedLuksDevice - %s", devPath)

	out, exitStatus, err := util.ExecCommandOutput("cryptsetup", []string{"status", "-v", devPath})
	log.Tracef("exit code of LUKS status: %d", exitStatus)
	if exitStatus == 0 {
		log.Infof("LUKS status output: %v", out)
		return true, nil
	}
	if exitStatus == 1 {
		log.Infof("not a mapped LUKS device - %s", devPath)
		return false, nil
	} else {
		err := fmt.Errorf("LUKS status is unsure of the device: %s. Returned code: %v with error: %v", devPath, exitStatus, err)
		log.Error(err.Error())
		return false, err
	}
}

// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

const (
	diskBySerial = "/dev/disk/by-id/"
	devDir       = "/dev/"
	// ScsiHostPathFormat
	ScsiHostPathFormat = "/sys/class/scsi_host/"
	// ScsiHostScanPathFormat
	ScsiHostScanPathFormat = "/sys/class/scsi_host/%s/scan"
)

// GetNimbleDmDevices : get the scsi device by specified serial
func GetDeviceBySerial(serialNumber string) (device *model.Device, err error) {
	log.Tracef(">>>>> GetDeviceBySerial called with serial %s", serialNumber)
	defer log.Traceln("<<<<< GetDeviceBySerial")

	files, err := ioutil.ReadDir(diskBySerial)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, f := range files {
		if !strings.Contains(f.Name(), serialNumber) {
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			pathName, err := os.Readlink(diskBySerial + f.Name())
			if err != nil {
				log.Errorf("unable to read symlink %s to get device details, err %s", f.Name(), err.Error())
				return nil, err
			}
			device := &model.Device{}
			// ../../sdb

			devNames := strings.Split(pathName, "/")
			if len(devNames) == 0 {
				return nil, fmt.Errorf("unable to get device details from symlink %s and path %s", diskBySerial+f.Name(), pathName)
			}
			device.Pathname = "/dev/" + devNames[len(devNames)-1]
			device.AltFullPathName = "/dev/" + devNames[len(devNames)-1]
			device.SerialNumber = serialNumber
			return device, nil
		}
	}
	// no device found
	return nil, nil
}

//AttachDevices : attached and creates scsi devices to host
func VmdkAttachDevices(vols []*model.Volume) (devs []*model.Device, err error) {
	log.Tracef(">>>>> VmdkAttachDevices")
	defer log.Traceln("<<<<< VmdkAttachDevices")

	// Get all SCSI host objects
	scsiHosts, err := GetScsiHosts()
	if err != nil {
		return nil, err
	}

	// Perform rescan
	err = RescanScsiHosts(scsiHosts, "0")
	if err != nil {
		return nil, err
	}

	// Give couple of seconds for device discovery to complete
	time.Sleep(time.Second * 2)

	for _, vol := range vols {
		device, err := GetDeviceBySerial(vol.SerialNumber)
		if err != nil {
			return nil, err
		}
		if device != nil {
			devs = append(devs, device)
		}
	}
	return devs, nil
}

// DeleteDevice : delete the multipath device
func VmdkDeleteDevice(dev *model.Device) (err error) {
	log.Tracef(">>>>> VmdkDeleteDevice called with %s", dev.SerialNumber)
	defer log.Traceln("<<<<< VmdkDeleteDevice")
	deletePath := fmt.Sprintf("/sys/block/%s/device/delete", strings.TrimPrefix(dev.Pathname, "/dev/"))
	exists, _, _ := util.FileExists(deletePath)
	if !exists {
		return nil
	}
	// cleanup device
	err = util.FileWriteString(deletePath, "1")
	if err != nil {
		return err
	}
	return nil
}

// GetScsiHosts returns all SCSI adapter host objects
func GetScsiHosts() ([]string, error) {
	log.Traceln(">>>>> GetScsiHosts")
	defer log.Traceln("<<<<< GetScsiHosts")
	exists, _, err := util.FileExists(ScsiHostPathFormat)
	if !exists {
		log.Errorf("no scsi hosts found")
		return nil, fmt.Errorf("no scsi hosts found")
	}

	listOfFiles, err := ioutil.ReadDir(ScsiHostPathFormat)
	if err != nil {
		log.Errorf("unable to get list of scsi hosts, error %s", err.Error())
		return nil, fmt.Errorf("unable to get list of scsi hosts, error %s", err.Error())
	}

	if len(listOfFiles) == 0 {
		return nil, nil
	}
	var ScsiHosts []string
	for _, file := range listOfFiles {
		log.Tracef("host name %s", file.Name())
		ScsiHosts = append(ScsiHosts, file.Name())
	}

	log.Tracef("scsiHosts %v", ScsiHosts)

	return ScsiHosts, nil
}

// RescanScsiHosts rescan's all SCSI host adapters
func RescanScsiHosts(scsiHosts []string, lunID string) (err error) {
	log.Traceln(">>>>> RescanScsiHosts")
	defer log.Traceln("<<<<< RescanScsiHosts")
	for _, scsiHost := range scsiHosts {
		if scsiHost == "" {
			continue
		}
		// perform rescan for all hosts
		log.Tracef("rescanHost initiated for %s", scsiHost)
		scsiHostScanPath := fmt.Sprintf(ScsiHostScanPathFormat, scsiHost)
		exists, _, _ := util.FileExists(scsiHostScanPath)
		if !exists {
			continue
		}
		if lunID == "" {
			err = util.FileWriteString(scsiHostScanPath, "- - -")
		} else {
			err = util.FileWriteString(scsiHostScanPath, "- - "+lunID)
		}
		if err != nil {
			log.Errorf("unable to rescan for scsi devices on host %s err %s", scsiHost, err.Error())
			return fmt.Errorf("unable to rescan for scsi devices on host %s err %s", scsiHost, err.Error())
		}
	}
	return nil
}

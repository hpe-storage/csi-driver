package nodeinit

import (
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/tunelinux"
)

func GetMultipathDevices(nodeName string) error {
	multipathDevices, err := tunelinux.GetMultipathDevices() //driver.GetMultipathDevices()
	if err != nil {
		log.Errorf("Error while getting the multipath devices on the node %s", nodeName)
		// This line will be uncommented once the full imeplementation of this stale entry fix is done
		//return err
		return nil //Remove later
	}
	if multipathDevices != nil && len(multipathDevices) > 0 {
		for _, device := range multipathDevices {
			//TODO: Assess whether the device belongs to this node or not and whether to do clean up or not
			log.Tracef("Name:%s Vendor:%s Paths:%f Path Faults:%f UUID:%s IsUnhealthy:%t", device.Name, device.Vendor, device.Paths, device.PathFaults, device.UUID, device.IsUnhealthy)
			//Remove Later
			if device.IsUnhealthy {
				log.Infof("Multipath device %s on the node %s is unhealthy", nodeName, device.Name)
			} else {
				log.Infof("Multipath device %s on the node %s is healthy", nodeName, device.Name)
			}
		}
	} else {
		log.Tracef("No multipath devices found on the node %s", nodeName)
	}
	return nil
}

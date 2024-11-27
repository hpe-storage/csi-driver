package nodeinit

import (
	"os"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/tunelinux"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	storage_v1 "k8s.io/api/storage/v1"
)

func doesDeviceBelongToTheNode(multipathDevice *model.MultipathDevice, volumeAttachmentList *storage_v1.VolumeAttachmentList, nodeName string) bool {
	if multipathDevice != nil {
		for _, va := range volumeAttachmentList.Items {
			log.Tracef("SERIAL NUMBER: %s, NAME: %s", va.Status.AttachmentMetadata["serialNumber"], va.Name)
			if multipathDevice.UUID[1:] == va.Status.AttachmentMetadata["serialNumber"] && nodeName == va.Spec.NodeName {
				return true
			}
		}
	}
	return false
}

func AnalyzeMultiPathDevices(flavor flavor.Flavor, nodeName string) error {
	log.Tracef(">>>>> AnalyzeMultiPathDevices for the node %s", nodeName)
	defer log.Trace("<<<<< AnalyzeMultiPathDevices")

	var disableCleanup bool
	var errCount int
	disableNodeMonitor := os.Getenv("DISABLE_NODE_MONITOR")
	if disableNodeMonitor == "true" {
		log.Infof("Node monitor is disabled, DISABLE_NODE_MONITOR=%v."+
			"Skipping the cleanup of stale multipath devices", disableNodeMonitor)
		disableCleanup = true
	}

	multipathDevices, err := tunelinux.GetMultipathDevices() //driver.GetMultipathDevices()
	if err != nil {
		log.Errorf("Error while getting the information of multipath devices on the node %s", nodeName)
		return err
	}

	if len(multipathDevices) == 0 {
		log.Infof("No multipath devices found on this node %s.", nodeName)
		return nil
	}

	log.Infof(" %d multipath devices found on the node %s", len(multipathDevices), nodeName)
	log.Tracef("Checking the connection to control plane....")
	if !flavor.CheckConnection() {
		log.Infof("The node %s is unable to connect to the control plane.", nodeName)
		if multipathDevices != nil && len(multipathDevices) > 0 {
			for _, device := range multipathDevices {
				log.Tracef("Name:%s Vendor:%s Paths:%d Path Faults:%d UUID:%s IsUnhealthy:%t", device.Name, device.Vend, device.Paths, device.PathFaults, device.UUID, device.IsUnhealthy)
				if device.IsUnhealthy {
					log.Infof("Multipath device %s on the node %s is unhealthy", device.Name, nodeName)
					err = cleanup(&device)
					if err != nil {
						log.Errorf("Unable to cleanup the multipath device %s: %s", device.Name, err.Error())
						errCount = errCount + 1
					}
				} else {
					log.Infof("Multipath device %s on the node %s is healthy", device.Name, nodeName)
				}
			}
			if errCount > 0 {
				log.Infof("Failed to remove %d multipath devices on the node %s", errCount, nodeName)
				return err
			}
			//No error while cleaninup or no devices are there to clean
			return nil
		}
		log.Tracef("No multipath devices found on the node %s", nodeName)
		return nil
	}
	log.Infof("Node %s has a proper connection with the control plane", nodeName)

	vaList, err := flavor.ListVolumeAttachments()
	if err != nil {
		return err
	}

	if vaList != nil {
		log.Infof("%d volume attachments found", len(vaList.Items))
	}

	for _, device := range multipathDevices {
		if vaList != nil && len(vaList.Items) > 0 {
			log.Infof("Assessing the multipath device %s", device.Name)
			if doesDeviceBelongToTheNode(&device, vaList, nodeName) {
				if device.IsUnhealthy {
					log.Infof("The multipath device %s belongs to this node %s and is unhealthy.", device.Name, nodeName)
				} else {
					log.Infof("The multipath device %s belongs to this node %s and is healthy.", device.Name, nodeName)
				}
			} else {
				if device.IsUnhealthy {
					log.Infof("The multipath device %s is unhealthy and either it does not belong to the node %s or is not created by the hpe csi driver.", device.Name, nodeName)
					//do cleanup
					if !disableCleanup {
						err = cleanup(&device)
						if err != nil {
							log.Errorf("Unable to cleanup the multipath device %s", device.Name)
							errCount = errCount + 1
						}
					} else {
						log.Warnf("Skipping the removal of stale multipath device %s as the DISABLE_NODE_MONITOR is set to %s", device.Name, disableNodeMonitor)
					}
				} else {
					log.Infof("The multipath device %s is healthy and either it does not belong to the node %s or is not created by the hpe csi driver.", device.Name, nodeName)
				}
			}
		} else {
			if device.IsUnhealthy {
				log.Infof("No volume attachments found. The multipath device is unhealthy and does not belong to the hpe csi driver, do clean up!")
				// Do cleanup
				if !disableCleanup {
					err = cleanup(&device)
					if err != nil {
						log.Errorf("Unable to cleanup the multipath device %s", device.Name)
						errCount = errCount + 1
					}
				} else {
					log.Warnf("Skipping the removal of stale multipath device %s as the DISABLE_NODE_MONITOR is set to %s", device.Name, disableNodeMonitor)
				}
			} else {
				log.Infof("No volume attachments found. The multipath device is healthy and does not belong to hpe csi driver.")
				//Nothing to do
			}
		}
	} // end-for loop

	if errCount > 0 {
		log.Infof("Failed to remove %d multipath devices on the node %s", errCount, nodeName)
		return err
	}
	return nil
}

func cleanup(device *model.MultipathDevice) error {
	log.Tracef(">>>>> Cleaning up the multipath device %s", device.Name)
	defer log.Trace("<<<<< cleanup")

	//unmount references & kill processes explicitly, if umount fails
	err := tunelinux.UnmountMultipathDevice(device.Name)
	if err != nil {
		log.Errorf("Unable to unmount the multipath device's references %s: %s", device.Name, err.Error())
		return err
	}

	//remove block devices of multipath device
	err = tunelinux.RemoveBlockDevicesOfMultipathDevices(*device)
	if err != nil {
		log.Errorf("Unable to remove the block devices of multipath device %s: %s", device.Name, err.Error())
		return err
	}
	//flush the multipath device
	err = tunelinux.RemoveMultipathDevice(device.Name)
	if err != nil {
		log.Errorf("Unable to remove the multipath device %s: %s", device.Name, err.Error())
		return err
	}
	log.Infof("The multipath device %s is removed successfully", device.Name)
	return nil
}

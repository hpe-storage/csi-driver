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
			log.Info("NAME:", va.Name, "PV:", *va.Spec.Source.PersistentVolumeName, "STATUS: ", va.Status)
			log.Info("ATTATCHMENTMETADATA: ", va.Status.AttachmentMetadata, "SERIAL NUMBER: ", va.Status.AttachmentMetadata["serialNumber"])
			log.Info("NODE NAME:", va.Spec.NodeName)

			if multipathDevice.UUID[1:] == va.Status.AttachmentMetadata["serialNumber"] && nodeName == va.Spec.NodeName {
				return true
			}
		}
	}
	return false
}

func AnalyzeMultiPathDevices(flavor flavor.Flavor, nodeName string) error {
	log.Trace(">>>>> AnalyzeMultiPathDevices for the node ", nodeName)
	defer log.Trace("<<<<< AnalyzeMultiPathDevices")

	var disableCleanup bool
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
	if multipathDevices != nil && len(multipathDevices) > 0 {
		for _, device := range multipathDevices {
			//TODO: Assess whether the device belongs to this node or not and whether to do clean up or not
			log.Tracef("Name:%s Vendor:%s Paths:%f Path Faults:%f UUID:%s IsUnhealthy:%t", device.Name, device.Vend, device.Paths, device.PathFaults, device.UUID, device.IsUnhealthy)
			//Remove Later
			if device.IsUnhealthy {
				log.Infof("Multipath device %s on the node %s is unhealthy", device.Name, nodeName)
			} else {
				log.Infof("Multipath device %s on the node %s is healthy", device.Name, nodeName)
			}
		}
	} else {
		log.Tracef("No multipath devices found on the node %s", nodeName)
		return nil
	}

	vaList, err := flavor.ListVolumeAttachments()
	if err != nil {
		return err
	}

	var err_count int
	for _, device := range multipathDevices {
		if vaList != nil && len(vaList.Items) > 0 {
			log.Infof("Assessing the multipath device %s", device.Name)
			if doesDeviceBelongToTheNode(device, vaList, nodeName) {
				log.Infof("Multipath device %s belongs to the node %s", device.Name, nodeName)
				if device.IsUnhealthy {
					log.Infof("The multipath device %s belongs to this node %s and is unhealthy. Issue warnings!", device.Name, nodeName)
				} else {
					log.Infof("The multipath device %s belongs to this node %s and is healthy. Nothing to do", device.Name, nodeName)
				}
			} else {
				log.Infof("Multipath device %s does not not belong to the node %s", device.Name, nodeName)
				if device.IsUnhealthy {
					log.Infof("The multipath device %s is unhealthy and it does not belong to the node %s", device.Name, nodeName)
					//do cleanup
					if !disableCleanup {
						log.Infof("Cleaning the stale multipath device %s as the DISABLE_NODE_MONITOR is set", device.Name)
						err = cleanup(device)
						if err != nil {
							log.Errorf("Unable to cleanup the multipath device %s", device.Name)
							err_count = err_count + 1
						}
					} else {
						log.Warnf("Skipping the removal of stale multipath device %s as the DISABLE_NODE_MONITOR is set to %s", device.Name, disableNodeMonitor)
					}
				} else {
					log.Infof("The multipath device %s is healthy and it does not belong to the node %s. Issue warnings!", device.Name, nodeName)
				}
			}
		} else {
			if device.IsUnhealthy {
				log.Infof("No volume attachments found. The multipath device is unhealthy and does not belong to the hpe csi driver, do cleanup!")
				// Do cleanup
				if !disableCleanup {
					log.Infof("Cleaning the stale multipath device %s as the DISABLE_CLEANUP is set", device.Name)
					err = cleanup(device)
					if err != nil {
						log.Errorf("Unable to cleanup the multipath device %s", device.Name)
						err_count = err_count + 1
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

	if err_count > 0 {
		log.Infof("Failed to remove %d multipath devices on the node %s", err_count, nodeName)
		return err
	}
	return nil
}

func cleanup(device *model.MultipathDevice) error {
	log.Tracef(">>>>> cleanup of the device %s", device.Name)
	defer log.Trace("<<<<< cleanup")

	//unmount references & kill processes explicitly, if umount fails
	err := tunelinux.UnmountMultipathDevice(device.Name)
	if err != nil {
		log.Errorf("Unable to unmount the multipath device's references%s: %s", device.Name, err.Error())
		return err
	}
	//remove block devices of multipath device
	err = tunelinux.RemoveBlockDevicesOfMultipathDevices(*device)
	if err != nil {
		log.Errorf("Unable to remove the block devices of multipath device %s: %s", device.Name, err.Error())
		return err
	}
	//flush the multipath device
	err = tunelinux.FlushMultipathDevice(device.Name)
	if err != nil {
		log.Errorf("Unable to flush the multipath device %s: %s", device.Name, err.Error())
		return err
	}
	return nil
}

package nodeinit

import (
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/tunelinux"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	storage_v1 "k8s.io/api/storage/v1"
)

func doesDeviceBelongToTheNode(multipathDevice *model.MultipathDeviceInfo, volumeAttachmentList *storage_v1.VolumeAttachmentList, nodeName string) bool {
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
	log.Trace(">>>>> AnalyzeMultiPathDevices")
	defer log.Trace("<<<<< AnalyzeMultiPathDevices")

	multipathDevices, err := tunelinux.GetMultipathDevices() //driver.GetMultipathDevices()
	if err != nil {
		log.Errorf("Error while getting the information of multipath devices on the node %s", nodeName)
		return err
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
		return nil
	}

	vaList, err := flavor.ListVolumeAttachments()
	if err != nil {
		return err
	}
	for _, device := range multipathDevices {
		if vaList != nil && len(vaList.Items) > 0 {
			log.Infof("Assessing the multipath device %s", device.Name)
			if doesDeviceBelongToTheNode(device, vaList, nodeName) {
				log.Infof("Multipath device %s belongs to the node %s", device.Name, nodeName)
				if device.IsUnhealthy {
					log.Info("The multipath device %s belongs to this node %s and is unhealthy. Issue warnings!", device.Name, nodeName)
				} else {
					log.Info("The multipath device %s belongs to this node %s and is healthy. Nothing to do", device.Name, nodeName)
				}
			} else {
				log.Infof("Multipath device %s doesnt not belong to the node %s", device.Name, nodeName)
				if device.IsUnhealthy {
					log.Infof("The multipath device %s is unhealthy and it does not belong to the node %s", device.Name, nodeName)
					//do cleanup
				} else {
					log.Infof("The multipath device %s is healthy and it does not belong to the node %s. Issue warnings!", device.Name, nodeName)
				}
			}
		} else {
			if device.IsUnhealthy {
				log.Infof("No volume attachments found. The multipath device is unhealthy and does not belong to HPE CSI driver, Do cleanup!")
				// Do cleanup
			} else {
				log.Infof("No volume attachmenst found. The multipath device is healthy and does not belong to HPE CSI driver")
				//Nothing to do
			}
		}
	} // end-for loop
	return nil
}

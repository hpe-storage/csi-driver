package initcontainer

import (
	"os"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/tunelinux"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/kubernetes"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
)

type InitContainer struct {
	flavor   flavor.Flavor
	nodeName string
}

func NewInitContainer(flavorName string, nodeService bool) *InitContainer {
	var initFlavour flavor.Flavor
	if flavorName == flavor.Kubernetes {
		flavor, err := kubernetes.NewKubernetesFlavor(nodeService, nil)
		if err != nil {
			return nil
		}
		initFlavour = flavor
	} else {
		initFlavour = &vanilla.Flavor{}
	}
	ic := &InitContainer{flavor: initFlavour}
	if key := os.Getenv("NODE_NAME"); key != "" {
		ic.nodeName = key
	}
	log.Infof("InitContainer: %+v", ic)
	// initialize InitContainer
	return ic
}
func (ic *InitContainer) Init() error {

	log.Trace(">>>>> init method of Init Container")
	defer log.Trace("<<<<< init method of Init Container")

	multipathDevices, err := tunelinux.GetMultipathDevices() //driver.GetMultipathDevices()
	if err != nil {
		log.Errorf("Error while getting the multipath devices")
		// This line will be uncommented once the full imeplementation of this stale entry fix is done
		//return err
		return nil //Remove later
	}
	if multipathDevices != nil && len(multipathDevices) > 0 {
		for _, device := range multipathDevices {
			//TODO: Assess whether the device belongs to this node or not and whether to do clean up or not
			log.Tracef("Name:%s Vendor:%s Paths:%f Path Faults:%f UUID:%s IsUnhealthy:%t", device.Name, device.Vendor, device.Paths, device.PathFaults, device.UUID.device.IsUnhealthy)
			//Remove Later
			if device.IsUnhealthy {
				log.Infof("Multipath device %s is unhealthy and is present on %s node", device.Name, ic.nodeName)
			} else {
				log.Infof("Multipath device %s is healthy and is present on %s node", device.Name, ic.nodeName)
			}
		}
	} else {
		log.Tracef("No multipath devices found on the node %s", ic.nodeName)
	}
	return nil
}

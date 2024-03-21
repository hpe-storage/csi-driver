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
		unhealthyDevices, err := tunelinux.GetUnhealthyMultipathDevices(multipathDevices)
		if err != nil {
			log.Errorf("Error while getting the unhealthy multipath devices: %s", err.Error())
			// This line will be uncommented once the full imeplementation of this stale entry fix is done
			//return err
			return nil // Remove later
		}
		log.Tracef("Unhealthy multipath devices found are: %+v", unhealthyDevices)

		if len(unhealthyDevices) > 0 {
			log.Tracef("Unhealthy multipath devices found on the node %s", ic.nodeName)
			//Do cleanup
		} else {
			log.Tracef("No unhealthy multipath devices found on the node %s", ic.nodeName)
		}
	} else {
		log.Tracef("No multipath devices found on the node %s", ic.nodeName)
	}
	return nil
}

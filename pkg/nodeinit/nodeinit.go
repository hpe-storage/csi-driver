package nodeinit

import (
	"os"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/kubernetes"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
)

type NodeInitContainer struct {
	flavor   flavor.Flavor
	nodeName string
}

func NewNodeInitContainer(flavorName string, nodeService bool) *NodeInitContainer {
	var nodeInitFlavour flavor.Flavor
	if flavorName == flavor.Kubernetes {
		flavor, err := kubernetes.NewKubernetesFlavor(nodeService, nil)
		if err != nil {
			return nil
		}
		nodeInitFlavour = flavor
	} else {
		nodeInitFlavour = &vanilla.Flavor{}
	}
	nic := &NodeInitContainer{flavor: nodeInitFlavour}
	if key := os.Getenv("NODE_NAME"); key != "" {
		nic.nodeName = key
	}
	log.Debugf("NodeInitContainer: %+v", nic)
	// initialize NodeInitContainer
	return nic
}

func (nic *NodeInitContainer) NodeInit() error {

	log.Trace(">>>>> node init container")
	defer log.Trace("<<<<< node init container")

	err := GetMultipathDevices(nic.nodeName)
	if err != nil {
		log.Errorf("Error occured while fetching the multipath devices on the node %s", nic.nodeName)
		return err
	}
	return nil
}


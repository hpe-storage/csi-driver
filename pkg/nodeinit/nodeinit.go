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
	return nic
}

func (nic *NodeInitContainer) NodeInit() error {

	log.Info(">>>>> node init container ", nic.nodeName)
	defer log.Trace("<<<<< node init container")

	err := AnalyzeMultiPathDevices(nic.flavor, nic.nodeName)
	if err != nil {
		log.Errorf("Error while analyzing the multipath devices %s on the node %s", err.Error(), nic.nodeName)
		return err
	}
	return nil
}

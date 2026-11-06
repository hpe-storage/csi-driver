package nodeinit

import (
	"errors"
	"fmt"
	"os"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/kubernetes"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
)

// ErrNoFlavor is returned by NodeInit when the orchestrator flavor could not be
// created (for the Kubernetes flavor: the API server was unreachable).
var ErrNoFlavor = errors.New("node init: orchestrator flavor is not available (is the API server reachable?)")

//nolint:revive
type NodeInitContainer struct {
	flavor   flavor.Flavor
	nodeName string
}

// NewNodeInitContainer builds the init container helper. When the Kubernetes
// flavor cannot be created the returned container has a nil flavor and
// NodeInit reports ErrNoFlavor; callers no longer have to nil-check the result.
//
// ESC-18251 fix verification F9: the previous version returned nil here and
// the caller dereferenced it at NodeInit(), so an unreachable API server ended
// the init container with a Go panic instead of a message.
func NewNodeInitContainer(flavorName string) *NodeInitContainer {
	var nodeInitFlavour flavor.Flavor
	if flavorName == flavor.Kubernetes {
		flavor, err := kubernetes.NewKubernetesFlavor(true, nil)
		if err != nil {
			log.Errorf("node init: unable to create the kubernetes flavor: %v", err)
		} else {
			nodeInitFlavour = flavor
		}
	} else {
		nodeInitFlavour = &vanilla.Flavor{}
	}
	nic := &NodeInitContainer{flavor: nodeInitFlavour}
	// NODE_NAME is injected by the node DaemonSet through the downward API
	// (spec.nodeName). Without it the attachment lookup below runs with an
	// empty name and finds "0 volume attachments" on nodes that have hundreds
	// (ESC-18251 parent D5 / fix verification A8), so warn loudly.
	if key := os.Getenv("NODE_NAME"); key != "" {
		nic.nodeName = key
	} else {
		log.Warn("node init: NODE_NAME is empty; per-node volume attachment accounting and orphan multipath device clean-up cannot work. Pass spec.nodeName to the init container.")
	}
	log.Debugf("NodeInitContainer: %+v", nic)
	return nic
}

// NodeInit runs the one-shot node initialisation: it reconciles the node's
// multipath devices against the VolumeAttachments recorded for this node.
// It returns ErrNoFlavor when the orchestrator flavor is not available.
func (nic *NodeInitContainer) NodeInit() error {
	if nic == nil || nic.flavor == nil {
		return ErrNoFlavor
	}

	log.Infof(">>>>> node init container on node %q", nic.nodeName)
	defer log.Trace("<<<<< node init container")

	err := AnalyzeMultiPathDevices(nic.flavor, nic.nodeName)
	if err != nil {
		log.Errorf("Error while analyzing the multipath devices %s on the node %s", err.Error(), nic.nodeName)
		return fmt.Errorf("analyze multipath devices on node %q: %w", nic.nodeName, err)
	}
	return nil
}

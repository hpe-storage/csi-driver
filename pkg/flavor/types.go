// Copyright 2019 Hewlett Packard Enterprise Development LP

package flavor

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/model"
)

const (
	// Vanilla flavored CSI driver
	Vanilla = "vanilla"

	// Kubernetes flavored CSI driver
	Kubernetes = "kubernetes"
)

// Flavor defines the interface for the type of driver
type Flavor interface {
	ConfigureAnnotations(claimName string, parameters map[string]string) (map[string]string, error)

	LoadNodeInfo(*model.Node) (string, error)
	UnloadNodeInfo()
	GetNodeInfo(nodeID string) (*model.Node, error)
	GetCredentialsFromPodSpec(volumeHandle string, podName string, namespace string) (map[string]string, error)
	GetCredentialsFromSecret(name string, namespace string) (map[string]string, error)

	CreateMultiNodeVolume(pvName string, reqVolSize int64) (multiWriterVolume *csi.Volume, rollback bool, err error)
	DeleteMultiNodeVolume(pvName string) error
	HandleMultiNodeNodePublish(request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error)
	IsMultiNodeVolume(volumeID string) bool
}

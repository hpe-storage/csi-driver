// Copyright 2019 Hewlett Packard Enterprise Development LP

package flavor

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/model"
	"k8s.io/api/core/v1"
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

	CreateMultiWriterVolume(request *csi.CreateVolumeRequest) (multiWriterVolume *csi.Volume, rollback bool, err error)
	DeleteMultiWriterVolume(claimName string) error
	HandleMultiWriterNodePublish(request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error)
	IsMultiWriterVolume(volumeID string) bool
	GetMultiWriterClaimFromClaimUID(uid string) (*v1.PersistentVolumeClaim, error)
}

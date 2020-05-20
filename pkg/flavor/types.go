// Copyright 2019 Hewlett Packard Enterprise Development LP

package flavor

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/model"
	"k8s.io/apimachinery/pkg/version"
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
	GetEphemeralVolumeSecretFromPod(volumeHandle string, podName string, namespace string) (string, error)
	GetCredentialsFromSecret(name string, namespace string) (map[string]string, error)
	IsPodExists(uid string) (bool, error)
	CreateNFSVolume(pvName string, reqVolSize int64, parameters map[string]string, volumeContentSource *csi.VolumeContentSource) (nfsVolume *csi.Volume, rollback bool, err error)
	DeleteNFSVolume(pvName string) error
	RollbackNFSResources(nfsResourceName, nfsNamespace string) error
	HandleNFSNodePublish(request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error)
	IsNFSVolume(volumeID string) bool
	GetVolumePropertyOfPV(propertyName string, pvName string) (string, error)
	GetNFSVolumeID(volumeID string) (string, error)
	GetOrchestratorVersion() (*version.Info, error)
	MonitorPod(podLabelkey, podLabelvalue string) error
}

// Copyright 2019 Hewlett Packard Enterprise Development LP

package flavor

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/model"
	storage_v1 "k8s.io/api/storage/v1"
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
	GetNodeLabelsByName(name string) (map[string]string, error)
	GetEphemeralVolumeSecretFromPod(volumeHandle string, podName string, namespace string) (string, error)
	GetCredentialsFromVolume(name string) (map[string]string, error)
	GetCredentialsFromSecret(name string, namespace string) (map[string]string, error)
	IsPodExists(uid string) (bool, error)
	CreateNFSVolume(pvName string, reqVolSize int64, parameters map[string]string, volumeContentSource *csi.VolumeContentSource) (nfsVolume *csi.Volume, rollback bool, err error)
	DeleteNFSVolume(pvName string) error
	RollbackNFSResources(nfsResourceName, nfsNamespace string) error
	HandleNFSNodePublish(request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error)
	IsNFSVolume(volumeID string) bool
	GetVolumePropertyOfPV(propertyName string, pvName string) (string, error)
	GetNFSVolumeID(volumeID string) (string, error)
	ExpandNFSBackendVolume(nfsVolumeID string, newCapacity int64) error
	IsNFSVolumeExpandable(volumeId string) bool
	GetOrchestratorVersion() (*version.Info, error)
	MonitorPod(podLabelkey, podLabelvalue string) error
	GetGroupSnapshotNameFromSnapshotName(snapshotName string) (string, error)
	ListVolumeAttachments() (*storage_v1.VolumeAttachmentList, error)
	GetChapCredentials(volumeContext map[string]string) (*model.ChapInfo, error)
	CheckConnection() bool
}

// Copyright 2019, 2025 Hewlett Packard Enterprise Development LP

package vanilla

import (
	"encoding/json"
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	storage_v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/version"
)

// Flavor of the CSI driver
type Flavor struct {
}

// ConfigureAnnotations does nothing in the vanilla case
//
//nolint:revive
func (flavor *Flavor) ConfigureAnnotations(claimName string, parameters map[string]string) (map[string]string, error) {
	return parameters, nil
}

// GetGroupSnapshotNameFromSnapshotName does nothing in vanilla case
//
//nolint:revive
func (flavor *Flavor) GetGroupSnapshotNameFromSnapshotName(snapName string) (string, error) {
	return "", nil
}

// LoadNodeInfo loads a Node object into JSON in the vanilla case.  Note this might hit the 128 character limit
// in some other CO.  A new flavor will likely need to be added in that case.
func (flavor *Flavor) LoadNodeInfo(node *model.Node) (string, error) {
	nodeBytes, err := json.Marshal(node)
	if err != nil {
		return "", fmt.Errorf("Could not marshal Node struct")
	}
	return string(nodeBytes), nil
}

// UnloadNodeInfo does nothing in the vanilla case
func (flavor *Flavor) UnloadNodeInfo() {
}

//nolint:revive
func (flavor *Flavor) GetNodeLabelsByName(name string) (map[string]string, error) {
	return make(map[string]string), nil
}

// GetNodeInfo returns the node details required to publish a volume to a node
func (flavor *Flavor) GetNodeInfo(nodeID string) (*model.Node, error) {
	var node *model.Node
	err := json.Unmarshal([]byte(nodeID), &node)

	return node, err
}

// GetEphemeralVolumeSecretFromPod :
//
//nolint:revive
func (flavor *Flavor) GetEphemeralVolumeSecretFromPod(volumeHandle string, podName string, namespace string) (string, error) {
	return "", nil
}

// GetCredentialsFromVolume :
//
//nolint:revive
func (flavor *Flavor) GetCredentialsFromVolume(name string) (map[string]string, error) {
	return nil, nil
}

// GetCredentialsFromSecret :
//
//nolint:revive
func (flavor *Flavor) GetCredentialsFromSecret(name string, namespace string) (map[string]string, error) {
	return nil, nil
}

// IsPodExists :
//
//nolint:revive
func (flavor *Flavor) IsPodExists(uid string) (bool, error) {
	return false, nil
}

//nolint:revive
func (flavor *Flavor) CreateNFSVolume(pvName string, reqVolSize int64, parameters map[string]string, volumeContentSource *csi.VolumeContentSource) (nfsVolume *csi.Volume, rollback bool, err error) {
	return nil, false, fmt.Errorf("NFS provisioned volume is not supported for non-k8s environments")
}

//nolint:revive
func (flavor *Flavor) RollbackNFSResources(nfsResourceName, nfsNamespace string) error {
	return nil
}

//nolint:revive
func (flavor *Flavor) DeleteNFSVolume(pvName string) error {
	return fmt.Errorf("NFS provisioned volume is not supported for non-k8s environments")
}

//nolint:revive
func (flavor *Flavor) HandleNFSNodePublish(request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	return nil, status.Error(codes.Internal, "NFS provisioned volume is not supported for non-k8s environments")
}

//nolint:revive
func (flavor *Flavor) IsNFSVolume(volumeID string) bool {
	return false
}

//nolint:revive
func (flavor *Flavor) GetVolumePropertyOfPV(propertyName string, pvName string) (string, error) {
	return "", nil
}

func (flavor *Flavor) GetVolumeById(volumeId string) (*v1.PersistentVolume, error) {
	return nil, nil
}

//nolint:revive
func (flavor *Flavor) GetNFSVolumeID(volumeID string) (string, error) {
	return "", nil
}

//nolint:revive
func (flavor *Flavor) ExpandNFSBackendVolume(nfsVolumeID string, newCapacity int64) error {
	return nil
}

func (flavor *Flavor) GetOrchestratorVersion() (*version.Info, error) {
	return nil, nil
}

//nolint:revive
func (flavor *Flavor) MonitorPod(podLabelkey, podLabelvalue string) error {
	return nil
}

//nolint:revive
func (flavor *Flavor) GetChapCredentials(volumeContext map[string]string) (*model.ChapInfo, error) {
	return nil, nil
}

func (flavor *Flavor) ListVolumeAttachments() (*storage_v1.VolumeAttachmentList, error) {
	return nil, nil
}

func (flavor *Flavor) CheckConnection() bool {
	return false
}

//nolint:revive
func (flavor *Flavor) GetPVCByVolumeID(id string) (*v1.PersistentVolumeClaim, error) {
	return nil, nil
}

//nolint:revive
func (flavor *Flavor) HandleFileNodePublish(request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	return nil, status.Error(codes.Internal, "File provisioned volume is not supported for non-k8s environments")
}

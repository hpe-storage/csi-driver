// Copyright 2019 Hewlett Packard Enterprise Development LP

package fake

import (
	"errors"
	"fmt"

	"github.com/hpe-storage/common-host-libs/model"
)

// StorageProvider is an implementor of the StorageProvider interface
type StorageProvider struct {
	volumes   map[string]model.Volume
	snapshots map[string]model.Snapshot
}

// NewFakeStorageProvider returns a fake storage provider
func NewFakeStorageProvider() *StorageProvider {
	return &StorageProvider{
		volumes:   make(map[string]model.Volume),
		snapshots: make(map[string]model.Snapshot),
	}
}

// Initialize does nothing
func (provider *StorageProvider) Initialize() {

}

// SetNodeContext does nothing
func (provider *StorageProvider) SetNodeContext(node *model.Node) error {
	return nil
}

// GetNodeContext does nothing
func (provider *StorageProvider) GetNodeContext(nodeID string) (*model.Node, error) {
	return nil, nil
}

// CreateVolume returns a fake volume
func (provider *StorageProvider) CreateVolume(name string, size int64, opts map[string]interface{}) (*model.Volume, error) {
	if _, ok := provider.volumes[name]; ok {
		return nil, fmt.Errorf("Volume named %s already exists", name)
	}
	fakeVolume := model.Volume{
		ID:   name,
		Name: name,
		Size: size,
	}
	provider.volumes[name] = fakeVolume
	return &fakeVolume, nil
}

// CloneVolume returns a fake volume
func (provider *StorageProvider) CloneVolume(name, sourceID, snapshotID string, opts map[string]interface{}) (*model.Volume, error) {
	if _, ok := provider.volumes[name]; ok {
		return nil, fmt.Errorf("Volume named %s already exists", name)
	}

	var snapshot *model.Snapshot
	var err error
	if sourceID == "" {
		snapshot, err = provider.GetSnapshot(snapshotID)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			return nil, errors.New("Could not find snapshot with id " + snapshotID)
		}
	} else if snapshotID == "" {
		snapshotName := "testSnapshot"
		snapshot, err = provider.CreateSnapshot(snapshotName, sourceID)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			return nil, errors.New("Could not create new snapshot for volume clone")
		}
	}

	fakeClone := model.Volume{
		ID:         name,
		Name:       name,
		BaseSnapID: snapshot.ID,
	}
	provider.volumes[name] = fakeClone
	return &fakeClone, nil
}

// DeleteVolume removes a fake volume
func (provider *StorageProvider) DeleteVolume(id string) error {
	if _, ok := provider.volumes[id]; ok {
		delete(provider.volumes, id)
		return nil
	}

	return fmt.Errorf("Could not find volume with id %s", id)
}

// PublishVolume returns fake publish data
func (provider *StorageProvider) PublishVolume(id, nodeID string) (*model.PublishInfo, error) {
	return &model.PublishInfo{
		SerialNumber: "eui.fake",
	}, nil
}

// UnpublishVolume does nothing
func (provider *StorageProvider) UnpublishVolume(id, nodeID string) error {
	return nil
}

// GetVolume returns a fake volume from memory
func (provider *StorageProvider) GetVolume(id string) (*model.Volume, error) {
	if _, ok := provider.volumes[id]; ok {
		fakeVolume := provider.volumes[id]
		return &model.Volume{
			ID:   fakeVolume.ID,
			Name: fakeVolume.Name,
			Size: fakeVolume.Size,
		}, nil
	}

	return nil, nil
}

// GetVolumeByName returns a fake volume from memory
func (provider *StorageProvider) GetVolumeByName(name string) (*model.Volume, error) {
	return provider.GetVolume(name)
}

// GetVolumes returns the fake volumes saved in the map
func (provider *StorageProvider) GetVolumes() ([]*model.Volume, error) {
	var volumes []*model.Volume

	for _, volume := range provider.volumes {
		fakeVolume := &model.Volume{
			ID:   volume.ID,
			Name: volume.Name,
			Size: volume.Size,
		}
		volumes = append(volumes, fakeVolume)
	}

	return volumes, nil
}

// GetSnapshots returns the fake snapshots saved in the map
func (provider *StorageProvider) GetSnapshots(sourceID string) ([]*model.Snapshot, error) {
	var snapshots []*model.Snapshot

	for _, snapshot := range provider.snapshots {
		fakeSnapshot := &model.Snapshot{
			ID:         snapshot.ID,
			Name:       snapshot.Name,
			VolumeID:   snapshot.VolumeID,
			VolumeName: snapshot.VolumeName,
			Size:       snapshot.Size,
			ReadyToUse: snapshot.ReadyToUse,
		}
		snapshots = append(snapshots, fakeSnapshot)
	}

	return snapshots, nil
}

// GetSnapshotByName returns a fake snapshot from memory
func (provider *StorageProvider) GetSnapshotByName(name string, sourceID string) (*model.Snapshot, error) {
	return provider.GetSnapshot(name)
}

// GetSnapshot returns the fake snapshot from memory
func (provider *StorageProvider) GetSnapshot(id string) (*model.Snapshot, error) {
	if _, ok := provider.snapshots[id]; ok {
		fakeSnap := provider.snapshots[id]
		return &model.Snapshot{
			ID:         fakeSnap.ID,
			Name:       fakeSnap.Name,
			VolumeID:   fakeSnap.VolumeID,
			VolumeName: fakeSnap.VolumeName,
			Size:       fakeSnap.Size,
			ReadyToUse: fakeSnap.ReadyToUse,
		}, nil
	}
	return nil, nil
}

// CreateSnapshot returns a fake snapshot
func (provider *StorageProvider) CreateSnapshot(name, sourceID string) (*model.Snapshot, error) {
	if _, ok := provider.snapshots[name]; ok {
		return nil, fmt.Errorf("Snapshot named %s already exists", name)
	}
	fakeSnapshot := model.Snapshot{
		ID:         name,
		Name:       name,
		VolumeID:   sourceID,
		VolumeName: sourceID,
		ReadyToUse: true,
	}
	provider.snapshots[name] = fakeSnapshot
	return &fakeSnapshot, nil
}

// DeleteSnapshot removes a fake volume
func (provider *StorageProvider) DeleteSnapshot(id string) error {
	if _, ok := provider.snapshots[id]; ok {
		delete(provider.snapshots, id)
		return nil
	}

	return fmt.Errorf("Could not find snapshot with id %s", id)
}

// ExpandVolume will expand the fake volume to requested size
func (provider *StorageProvider) ExpandVolume(id string, requestBytes int64) (*model.Volume, error) {
	if _, ok := provider.volumes[id]; !ok {
		return nil, fmt.Errorf("Could not find volume with id %s", id)
	}
	fakeVolume := model.Volume{
		ID:   id,
		Size: requestBytes,
	}
	// update volume in the map, so that new size will be reflected
	provider.volumes[id] = fakeVolume
	return &fakeVolume, nil
}

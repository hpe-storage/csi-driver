// Copyright 2019 Hewlett Packard Enterprise Development LP

package fake

import (
	"errors"
	"fmt"

	"github.com/hpe-storage/common-host-libs/model"
)

// StorageProvider is an implementor of the StorageProvider interface
type StorageProvider struct {
	volumes        map[string]model.Volume
	snapshots      map[string]model.Snapshot
	volumeGroups   map[string]model.VolumeGroup
	snapshotGroups map[string]model.SnapshotGroup
}

// NewFakeStorageProvider returns a fake storage provider
func NewFakeStorageProvider() *StorageProvider {
	return &StorageProvider{
		volumes:        make(map[string]model.Volume),
		snapshots:      make(map[string]model.Snapshot),
		volumeGroups:   make(map[string]model.VolumeGroup),
		snapshotGroups: make(map[string]model.SnapshotGroup),
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
func (provider *StorageProvider) CreateVolume(name, description string, size int64, opts map[string]interface{}) (*model.Volume, error) {
	if _, ok := provider.volumes[name]; ok {
		return nil, fmt.Errorf("Volume named %s already exists", name)
	}
	fakeVolume := model.Volume{
		ID:     name,
		Name:   name,
		Size:   size,
		Config: opts,
	}
	provider.volumes[name] = fakeVolume
	return &fakeVolume, nil
}

// CreateVolumeGroup returns a fake volume group
func (provider *StorageProvider) CreateVolumeGroup(name, description string, opts map[string]interface{}) (*model.VolumeGroup, error) {
	if _, ok := provider.volumeGroups[name]; ok {
		return nil, fmt.Errorf("Volume Group named %s already exists", name)
	}
	fakeVolumeGroup := model.VolumeGroup{
		ID:     name,
		Name:   name,
		Config: opts,
	}
	provider.volumeGroups[name] = fakeVolumeGroup
	return &fakeVolumeGroup, nil
}

// CreateSnapshotGroup returns a fake volume group
func (provider *StorageProvider) CreateSnapshotGroup(name, sourceVolumeGroupID string, opts map[string]interface{}) (*model.SnapshotGroup, error) {
	if _, ok := provider.snapshotGroups[name]; ok {
		return nil, fmt.Errorf("Snapshot Group named %s already exists", name)
	}
	fakeSnapshotGroup := model.SnapshotGroup{
		ID:                  name,
		Name:                name,
		SourceVolumeGroupID: sourceVolumeGroupID,
	}
	provider.snapshotGroups[name] = fakeSnapshotGroup
	return &fakeSnapshotGroup, nil
}

// CloneVolume returns a fake volume
func (provider *StorageProvider) CloneVolume(name, description, sourceID, snapshotID string, size int64, opts map[string]interface{}) (*model.Volume, error) {
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
		snapshot, err = provider.CreateSnapshot(snapshotName, snapshotName, sourceID, nil)
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
		Size:       size,
		BaseSnapID: snapshot.ID,
	}
	provider.volumes[name] = fakeClone
	return &fakeClone, nil
}

// DeleteVolume removes a fake volume
func (provider *StorageProvider) DeleteVolume(id string, force bool) error {
	if _, ok := provider.volumes[id]; ok {
		delete(provider.volumes, id)
		return nil
	}

	return fmt.Errorf("Could not find volume with id %s", id)
}

// DeleteVolumeGroup removes a fake volumeGroup
func (provider *StorageProvider) DeleteVolumeGroup(id string) error {
	if _, ok := provider.volumeGroups[id]; ok {
		delete(provider.volumeGroups, id)
		return nil
	}
	return fmt.Errorf("Could not find volume group with id %s", id)
}

// DeleteSnapshotGroup removes a fake snapshotGroup
func (provider *StorageProvider) DeleteSnapshotGroup(id string) error {
	if _, ok := provider.snapshotGroups[id]; ok {
		delete(provider.snapshotGroups, id)
		return nil
	}
	return fmt.Errorf("Could not find snapshot group with id %s", id)
}

// PublishVolume returns fake publish data
func (provider *StorageProvider) PublishVolume(id, hostUUID, accessProtocol string) (*model.PublishInfo, error) {
	return &model.PublishInfo{
		SerialNumber: "eui.fake",
	}, nil
}

// UnpublishVolume does nothing
func (provider *StorageProvider) UnpublishVolume(id, hostUUID string) error {
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
func (provider *StorageProvider) CreateSnapshot(name, description, sourceID string, opts map[string]interface{}) (*model.Snapshot, error) {
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
	fakeVolume := provider.volumes[id]
	// update volume, so that new size will be reflected
	fakeVolume.Size = requestBytes
	return &fakeVolume, nil
}

// EditVolume will edit the fake volume with requested params
func (provider *StorageProvider) EditVolume(id string, parameters map[string]interface{}) (*model.Volume, error) {
	if _, ok := provider.volumes[id]; !ok {
		return nil, fmt.Errorf("Could not find volume with id %s", id)
	}

	// update volume in the map, so that new properties will be reflected
	fakeVolume := provider.volumes[id]
	for key, value := range parameters {
		fakeVolume.Config[key] = value
	}
	return &fakeVolume, nil
}

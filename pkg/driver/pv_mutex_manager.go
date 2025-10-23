// Copyright 2025 Hewlett Packard Enterprise Development LP

package driver

import (
	"sync"

	log "github.com/hpe-storage/common-host-libs/logger"
)

// PVMutexManager manages volume-specific mutexes for PV annotation updates
// This ensures that concurrent ControllerPublish/Unpublish operations on the same
// RWX file volume don't cause race conditions when updating PV annotations
type PVMutexManager struct {
	volumeMutexMap sync.Map // map[string]*sync.Mutex - thread-safe map for volume-specific mutexes
}

// NewPVMutexManager creates a new PVMutexManager instance
func NewPVMutexManager() *PVMutexManager {
	return &PVMutexManager{}
}

// GetVolumeMutex retrieves or creates a mutex for a specific volume
// Uses sync.Map for lock-free reads when mutex already exists
func (m *PVMutexManager) GetVolumeMutex(volumeID string) *sync.Mutex {
	// Try to load existing mutex
	if value, ok := m.volumeMutexMap.Load(volumeID); ok {
		return value.(*sync.Mutex)
	}

	// Create new mutex and store it (LoadOrStore is atomic)
	mutex := &sync.Mutex{}
	actual, loaded := m.volumeMutexMap.LoadOrStore(volumeID, mutex)

	if !loaded {
		log.Tracef("Created new mutex for file volume %s", volumeID)
	}

	return actual.(*sync.Mutex)
}

// CleanupVolumeMutex removes the mutex for a specific volume from the map
// This should be called when a file volume is deleted to prevent memory leaks
func (m *PVMutexManager) CleanupVolumeMutex(volumeID string) {
	m.volumeMutexMap.Delete(volumeID)
	log.Tracef("Cleaned up mutex for file volume %s", volumeID)
}
